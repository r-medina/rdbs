package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	osuser "os/user"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"github.com/pkg/errors"
	"github.com/skratchdot/open-golang/open"
	"github.com/zmb3/spotify"

	"github.com/r-medina/rdbs"
)

var (
	dry         bool
	rekordbox   bool
	RekordboxDB = "/Users/%s/Library/Pioneer/rekordbox/master.db"
)

func help() {
	fmt.Println(`rdbs [-d] <playlist-name> <playlist-file>
	-d	dry run (only search song names - don't make playlist)`)
}

func init() {
	flag.BoolVar(&dry, "d", false, "dry run")
	flag.BoolVar(&rekordbox, "r", false, "use rekordbox")
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
	playlistLocation := flag.Args()[1]

	log.Printf("loading %q into spotify as %q", playlistLocation, playlistName)

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
		fmt.Println() // newline after secret
	}

	log.Println("opening browser to authenticate with spotify")

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

	var tracks []Track
	if !rekordbox {
		data, err := readData(playlistLocation)
		failIfError("could not read the playlist file: %+v", err)
		tracks, err = listTracks(data)
		failIfError("could not list tracks", err)
	} else {
		rekordboxDB := os.Getenv("REKORDBOX_DB")
		if rekordboxDB != "" {
			RekordboxDB = rekordboxDB
		} else {
			u, err := osuser.Current()
			failIfError("getting user for finding rekordbox db", err)
			RekordboxDB = fmt.Sprintf(RekordboxDB, u.Username)
		}

		log.Printf("opening db %s", RekordboxDB)
		db, err := rdbs.OpenDB(RekordboxDB)
		failIfError("opening rekordbox db", err)
		defer db.Close()

		log.Printf("opened rekordbox db")

		songs, err := rdbs.GetPlaylistSongs(db, playlistLocation)
		failIfError("reading playlist songs", err)

		log.Println("got songs")
		for _, song := range songs {
			tracks = append(tracks, Track{
				Artist: song.Artist,
				Title:  song.Title,
			})
		}
	}

	wg := sync.WaitGroup{}
	log.Println("searching for songs on spotify")
	for i, t := range tracks {
		wg.Add(1)
		go func(i int, t Track) {
			defer wg.Done()
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

			q := fmt.Sprintf("%s %s", artist, title)
			results, err := client.Search(q, spotify.SearchTypeTrack)
			if err != nil {
				log.Printf("spotify search failed: %+v", err)
				return
			}

			if results.Tracks != nil {
				if len(results.Tracks.Tracks) == 0 {
					log.Printf("could not find '%s - %s'", artist, title)
					return
				}
				track := results.Tracks.Tracks[0]

				if dry {
					fmt.Printf("\t%s - %s %q\n", track.Artists[0].Name, track.Name, track.ID)
				}

				tracks[i].SpotifyTitle = track.Name
				tracks[i].SpotifyID = track.ID
			}
		}(i, t)
	}
	wg.Wait()

	if !dry {
		log.Println("adding songs to playlist")

		// do this one by one to print errors better
		for _, track := range tracks {
			if track.SpotifyID == "" {
				continue
			}

			_, err = client.AddTracksToPlaylist(playlist.ID, track.SpotifyID)
			if err != nil {
				// grab the track name
				log.Printf("could not add track %q to playlist: %+v", track.SpotifyTitle, err)
			}

		}
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

	// print brower url in case it doesnt open automatically
	fmt.Printf("opening browser to %s\n", auth.AuthURL(""))
	if err := open.Run(auth.AuthURL("")); err != nil {
		return nil, err
	}

	select {
	case err := <-httpDone:
		return client, err
	case <-time.After(120 * time.Second):
		return nil, errors.New("timeout waiting for oauth token")
	}
}

type Track struct {
	Artist       string
	Title        string
	SpotifyTitle string
	SpotifyID    spotify.ID
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

func listTracks(data [][]string) ([]Track, error) {
	var tracks []Track
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

		tracks = append(tracks, Track{Artist: artist, Title: title})
	}

	return tracks, nil
}

func failIfError(msg string, err error) {
	if err == nil {
		return
	}

	log.Fatalf("%s: %v", msg, err)
}
