package main

import (
	"fmt"
	"log"
	"os"

	"github.com/manifoldco/promptui"

	"github.com/r-medina/rdbs/rekordbox"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if len(os.Args) < 2 {
		log.Fatalf("Usage: %s <db_location> <playlist_name>", os.Args[0])
	}

	playlistName := os.Args[1]

	log.Printf("getting %s", playlistName)

	db, err := rekordbox.OpenDB("/Users/ricky/Library/Pioneer/rekordbox/master.db")
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer db.Close()

	playlists, err := rekordbox.GetPlaylistInfo(db, playlistName)
	if err != nil {
		log.Fatalf("getting playlist ID: %v", err)
	}
	if len(playlists) == 0 {
		log.Fatal("no playlist found")
	}

	i := 0
	if len(playlists) > 1 {
		var formatted []string

		for _, p := range playlists {
			if p.ParentName != "" {
				formatted = append(formatted, fmt.Sprintf("%s (%s)", p.Name, p.ParentName))
			} else {
				formatted = append(formatted, fmt.Sprintf("%s (root)", p.Name))
			}
		}

		prompt := promptui.Select{
			Label:  "select a playlist (parent)",
			Items:  formatted,
			Stdout: os.Stderr, // so it doesn't get lost in redirects
		}
		i, _, err = prompt.Run()
		if err != nil {
			log.Fatalf("getting selecting playlist: %v", err)
		}
	}

	songs, err := rekordbox.GetPlaylistTracks(db, playlists[i].ID)
	if err != nil {
		log.Fatalf("getting playlist songs: %v", err)
	}

	// Print the songs for demonstration purposes.
	for _, song := range songs {
		fmt.Printf("%s - %s\n", song.Artist, song.Title)
	}
}
