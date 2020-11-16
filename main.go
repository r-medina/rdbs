package main

import (
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

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
		log.Println("executing dry run")
	}
	
	spotifyClientID := os.Getenv("SPOTIFY_ID")
	spotifySecret := os.Getenv("SPOTIFY_SECRET")

	if spotifyClientID == "" || spotifySecret == "" {
		log.Fatalf("please set environment variables SPOTIFY_ID and SPOTIFY_SECRET")
	}

	playlistName := flag.Args()[0]
	playlistFile := flag.Args()[1]

	client, err := oauthClient(spotifyClientID, spotifySecret)

	user, err := client.CurrentUser()
	if err != nil {
		log.Fatalf("could not get current user: %v", err)
	}
	log.Printf("user: %s", user.DisplayName)

	var playlist *spotify.FullPlaylist
	if !dry {
		playlist, err = client.CreatePlaylistForUser(user.ID, playlistName, "exported from rekordbox", false)
		if err != nil {
			log.Fatalf("could not create playlist %q: %v", playlistName, err)
		}
	}

	tracks, err := listTracks(playlistFile)
	if err != nil {
		log.Fatalf("failed to list tracks in playlist file %q: %v", playlistFile, err)
	}

	ids := []spotify.ID{}
	for _, t := range tracks {
		artist := t.Artist
		title := t.Title

		
		fmt.Printf("\t%s - %s\n", artist, title)

		// spotify doesnt like the (Original Mix) or (Someone
		// Remix) that dance music uses

		title = strings.ReplaceAll(title, "(Original Mix)", "")
		title = strings.ReplaceAll(title, "(", "")
		title = strings.ReplaceAll(title, ")", "")

		end := len(artist)
		if i := strings.Index(artist, "("); i >0 {
			end = i
		}
		artist = artist[0:end]
		
		q := fmt.Sprintf("%s %s\n", artist, title)
		results, err := client.Search(q, spotify.SearchTypeTrack)
		if err != nil {
			log.Fatalf("spotify search failed: %v", err)
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
		_, err = client.AddTracksToPlaylist(playlist.ID, ids...)
		if err != nil {
			log.Fatalf("could not add tracks to playlist %q: %v", playlistName, err)
		}
	}
}

func oauthClient(clientID, secretKey string) (*spotify.Client, error) {
	auth := spotify.NewAuthenticator("http://localhost:8666/", spotify.ScopePlaylistModifyPrivate)
	auth.SetAuthInfo(clientID, secretKey)

	var client *spotify.Client
	httpDone := make(chan error)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		defer close(httpDone)

		token, err := auth.Token("", r)
		if err != nil {
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

	err := <-httpDone
	return client, err
}

type track struct {
	Artist string
	Title  string
}

func listTracks(fname string) ([]track, error) {
	f, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	reader := csv.NewReader(f)
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1

	data, err := reader.ReadAll()
	if err != nil {
		return nil, err
	}

	var tracks []track
	for i, d := range data {
		if len(d) < 4 || i == 0 {
			continue
		}

		artist := d[4]
		title := d[2]

		// rekordbox includes a bunch of \x00 in the text :/
		artist = string(bytes.ReplaceAll([]byte(artist), []byte{00}, nil))
		title = string(bytes.ReplaceAll([]byte(title), []byte{00}, nil))

		artist = strings.TrimSpace(artist)
		title = strings.TrimSpace(title)

		tracks = append(tracks, track{Artist: artist, Title: title})
	}

	return tracks, nil
}
