package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"github.com/pkg/errors"
	"github.com/skratchdot/open-golang/open"
	"github.com/zmb3/spotify"
)

var dry bool

func help() {
	fmt.Println(`rdbs [-d] <playlist-name> <playlist-file>
	-d	dry run (only search song names - don't make playlist)`)
}

func init() {
	flag.BoolVar(&dry, "d", false, "dry run")
}

func main() {
	flag.Parse()

	if flag.NArg() < 2 {
		help()
		os.Exit(1)
	}

	if dry {
		log.SetFlags(log.Lshortfile)
		log.Println("executing dry run")
	}

	playlistName := flag.Args()[0]
	playlistFile := flag.Args()[1]

	log.Printf("loading %q into spotify as %q", playlistFile, playlistName)

	var client *spotify.Client
	var playlist *spotify.FullPlaylist
	spotifyClientID := os.Getenv("SPOTIFY_ID")
	spotifySecret := os.Getenv("SPOTIFY_SECRET")

	if spotifyClientID == "" {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("input your Spotify client ID: ")
		var err error
		spotifyClientID, err = reader.ReadString('\n')
		failIfError("error reading Spotify client ID", err)
		spotifyClientID = strings.TrimSuffix(spotifyClientID, "\n")
	}

	if spotifySecret == "" {
		fmt.Print("input your Spotify secret key: ")
		secretBytes, err := term.ReadPassword(int(syscall.Stdin))
		failIfError("error reading Spotify secret key", err)
		spotifySecret = string(secretBytes)
	}

	var err error
	client, err = oauthClient(spotifyClientID, spotifySecret)
	failIfError("oauth client failed", err)

	user, err := client.CurrentUser()
	failIfError("could not get current user", err)
	log.Printf("user: %s", user.DisplayName)

	if !dry {
		playlist, err = client.CreatePlaylistForUser(user.ID, playlistName, "exported from rekordbox", false)
		failIfError("could not create playlist", err)
	}

	data, err := readData(playlistFile)
	failIfError("could not read the playlist file: %+v", err)

	tracks, err := listTracks(data)
	failIfError("could not list tracks", err)

	ids := []spotify.ID{}
	for _, t := range tracks {
		artist := t.Artist
		title := t.Title

		fmt.Printf("\t%s - %s\n", artist, title)

		// spotify doesnt like the (Original Mix) or (Someone
		// Remix) that dance music uses
		// also doesnt like "feat"

		title = strings.ToLower(title)
		title = strings.ReplaceAll(title, "original mix", "")
		title = strings.ReplaceAll(title, "(", "")
		title = strings.ReplaceAll(title, ")", "")
		title = strings.ReplaceAll(title, "feat.", "")

		end := len(artist)
		if i := strings.Index(artist, "("); i > 0 {
			end = i
		}
		artist = artist[0:end]

		q := fmt.Sprintf("%s %s\n", artist, title)
		results, err := client.Search(q, spotify.SearchTypeTrack)
		if err != nil {
			log.Printf("spotify search failed: %+v", err)
			continue
		}

		if results.Tracks != nil {
			if len(results.Tracks.Tracks) == 0 {
				log.Printf("could not find '%s - %s'", artist, title)
				continue
			}
			track := results.Tracks.Tracks[0]

			if dry {
				fmt.Printf("\t\t%s - %s %q\n", track.Name, track.Artists[0].Name, track.ID)
			}
			ids = append(ids, track.ID)
		}
	}

	if !dry {
		log.Println("adding songs to playlist")

		// we have to break the request into chunks of 100
		// songs to do this, we loop over chunks of `ids` of
		// length 100 and then finish off the remaining ones
		// at the end

		for len(ids) > 100 {
			_, err = client.AddTracksToPlaylist(playlist.ID, ids[0:100]...)
			failIfError("could not add tracks to playlist", err)
			ids = ids[100:]
		}

		_, err = client.AddTracksToPlaylist(playlist.ID, ids...)
		failIfError("could not add tracks to playlist", err)
	}
}

func oauthClient(clientID, secretKey string) (*spotify.Client, error) {
	auth := spotify.NewAuthenticator("http://localhost:8666/", spotify.ScopePlaylistModifyPrivate)
	auth.SetAuthInfo(clientID, secretKey)

	var client *spotify.Client
	httpDone := make(chan error)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			log.Println(r.Method, r.URL.Path)
			return
		}
		defer close(httpDone)

		token, err := auth.Token("", r)
		if err != nil {
			log.Println("error getting token:", err)
			http.Error(w, "failed to get token", http.StatusNotFound)
			httpDone <- errors.Wrap(err, "failed to get token")
			return
		}
		c := auth.NewClient(token)
		client = &c

		fmt.Fprintf(w, `you may close this webpage`)
	})

	go func() {
		http.ListenAndServe("localhost:8666", nil)
	}()

	if err := open.Run(auth.AuthURL("")); err != nil {
		return nil, err
	}

	select {
	case err := <-httpDone:
		return client, err
	case <-time.After(60 * time.Second):
		return nil, errors.New("timeout waiting for oauth token")
	}
}

type track struct {
	Artist string
	Title  string
}

func readData(fname string) ([][]string, error) {
	original, err := os.Open(fname)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer original.Close()

	dec := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()
	utf8r := transform.NewReader(original, dec)
	csvr := csv.NewReader(utf8r)

	csvr.Comma = '\t'
	csvr.FieldsPerRecord = -1
	records, err := csvr.ReadAll()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return records, nil
}

func listTracks(data [][]string) ([]track, error) {
	var tracks []track
	var ia int
	var it int
	for i, d := range data {
		if i == 0 {
			for j, field := range d {
				if field == "Artist" {
					ia = j
				} else if field == "Track Title" {
					it = j
				}
			}

			continue
		}

		if len(d) < 4 {
			continue
		}

		artist := d[ia]
		title := d[it]

		artist = strings.TrimSpace(artist)
		title = strings.TrimSpace(title)

		tracks = append(tracks, track{Artist: artist, Title: title})
	}

	return tracks, nil
}

func failIfError(msg string, err error) {
	if err == nil {
		return
	}

	log.Fatalf("%s: %v", msg, err)
}
