package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/pkg/errors"
	"github.com/skratchdot/open-golang/open"
	"github.com/zmb3/spotify"
)

func help() {
	fmt.Println(`rdbs <playlist-name> <playlist-file>`)
}

func main() {
	if len(os.Args) < 3 {
		help()
		os.Exit(1)
	}

	spotifyClientID := os.Getenv("SPOTIFY_ID")
	spotifySecret := os.Getenv("SPOTIFY_SECRET")

	if spotifyClientID == "" || spotifySecret == "" {
		log.Fatalf("please set environment variables SPOTIFY_ID and SPOTIFY_SECRET")
	}

	playlistName := os.Args[1]
	playlistFile := os.Args[2]

	// config := &clientcredentials.Config{
	// 	ClientID:     spotifyClientID,
	// 	ClientSecret: spotifySecret,
	// 	TokenURL:     spotify.TokenURL,
	// }
	// token, err := config.Token(context.Background())
	// if err != nil {
	// 	log.Fatalf("couldn't get token: %v", err)
	// }

	client, err := oauthClient(spotifyClientID, spotifySecret)

	user, err := client.CurrentUser()
	if err != nil {
		log.Fatalf("could not get current user: %v", err)
	}
	fmt.Printf("user: %s\n", user.DisplayName)

	playlist, err := client.CreatePlaylistForUser(user.ID, playlistName, "exported from rekordbox", false)
	if err != nil {
		log.Fatalf("could not create playlist %q: %v", playlistName, err)
	}

	tracks, err := listTracks(playlistFile)
	if err != nil {
		log.Fatalf("failed to list tracks in playlist file %q: %v", playlistFile, err)
	}

	ids := []spotify.ID{}
	for _, t := range tracks {
		fmt.Printf("\t%s - %s\n", t.Artist, t.Title)

		q := fmt.Sprintf("%s %s\n", t.Artist, t.Title)
		results, err := client.Search(q, spotify.SearchTypeTrack)
		if err != nil {
			log.Fatalf("spotify search failed: %v", err)
		}

		if results.Tracks != nil {
			if len(results.Tracks.Tracks) == 0 {
				continue
			}
			ids = append(ids, results.Tracks.Tracks[0].ID)
		}
	}

	// for _, id := range ids {
	// 	fmt.Printf("https://open.spotify.com/track/%s\n", id)
	// }

	log.Println("adding songs to playlist")
	_, err = client.AddTracksToPlaylist(playlist.ID, ids...)
	if err != nil {
		log.Fatalf("could not add tracks to playlist %q: %v", playlistName, err)
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
		if len(d) < 2 || i == 0 {
			continue
		}

		artist := d[4]
		title := d[2]

		// rekordbox includes a bunch of \x00 in the text :/
		artist = string(bytes.ReplaceAll([]byte(artist), []byte{00}, nil))
		title = string(bytes.ReplaceAll([]byte(title), []byte{00}, nil))

		title = strings.TrimSpace(title)
		title = strings.ReplaceAll(title, "(Original Mix)", "")
		title = strings.ReplaceAll(title, "(", "")
		title = strings.ReplaceAll(title, ")", "")

		tracks = append(tracks, track{Artist: artist, Title: title})
	}

	return tracks, nil
}
