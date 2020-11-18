package main

import (
	"bytes"
	"encoding/csv"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"unicode"

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
	log.SetFlags(log.Lshortfile)
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

	playlistName := flag.Args()[0]
	playlistFile := flag.Args()[1]

	log.Printf("loading %q into spotify as %q", playlistFile, playlistName)

	var client *spotify.Client
	var playlist *spotify.FullPlaylist
	if !dry {
		spotifyClientID := os.Getenv("SPOTIFY_ID")
		spotifySecret := os.Getenv("SPOTIFY_SECRET")

		if spotifyClientID == "" || spotifySecret == "" {
			log.Fatalf("please set environment variables SPOTIFY_ID and SPOTIFY_SECRET")
		}

		var err error
		client, err = oauthClient(spotifyClientID, spotifySecret)

		user, err := client.CurrentUser()
		if err != nil {
			log.Fatalf("could not get current user: %v", err)
		}
		log.Printf("user: %s", user.DisplayName)

		playlist, err = client.CreatePlaylistForUser(user.ID, playlistName, "exported from rekordbox", false)
		if err != nil {
			log.Fatalf("could not create playlist %q: %v", playlistName, err)
		}
	}

	if err := preprocess(playlistFile); err != nil {
		log.Fatalf("processing %q: %v", playlistFile, err)
	}

	tracks, err := listTracks(playlistFile + ".new")
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
		if i := strings.Index(artist, "("); i > 0 {
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

func preprocess(playlistFile string) error {
	original, err := os.Open(playlistFile)
	if err != nil {
		return err
	}
	defer original.Close()

	r := csv.NewReader(original)
	r.Comma = '\t'
	r.FieldsPerRecord = -1

	out := [][]string{}
	data, err := r.ReadAll()
	for _, row := range data {
		if len(row) <= 1 { // some bad rows
			continue
		}
		newRow := []string{}
		for _, field := range row {
			field = strings.Map(func(r rune) rune {
				if unicode.IsPrint(r) || unicode.IsGraphic(r) {
					return r
				}
				return -1
			}, field)

			newRow = append(newRow, field)
		}

		out = append(out, newRow)
	}

	processed, err := os.Create(playlistFile + ".new")
	if err != nil {
		return err
	}
	defer processed.Close()

	w := csv.NewWriter(processed)
	w.Comma = '\t'
	return w.WriteAll(out)
}

func readData(fname string) ([][]string, error) {
	f, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1

	return reader.ReadAll()
}

func writeData(fname string, data [][]string) error {
	f, err := os.OpenFile(fname+"2", os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	f.Truncate(0)

	w := csv.NewWriter(f)
	w.Comma = '\t'

	// r := csv.NewReader()

	return w.WriteAll(data)
}

func listTracks(fname string) ([]track, error) {
	f, err := os.Open(fname)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	buf, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, err
	}

	buf = bytes.ReplaceAll(buf, []byte{00}, nil)
	r := bytes.NewReader(buf)

	reader := csv.NewReader(r)
	reader.Comma = '\t'
	reader.FieldsPerRecord = -1

	data, err := reader.ReadAll()

	// data, err := readData(fname)
	if err != nil {
		return nil, err
	}

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
