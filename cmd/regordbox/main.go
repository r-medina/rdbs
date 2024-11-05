package main

import (
	"fmt"
	"log"
	"os"

	_ "github.com/mutecomm/go-sqlcipher/v4"

	"github.com/r-medina/rdbs/rekordbox"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if len(os.Args) < 3 {
		log.Fatalf("Usage: %s <db_location> <playlist_name>", os.Args[0])
	}

	dbLocation := os.Args[1]
	playlistName := os.Args[2]

	db, err := rekordbox.OpenDB(dbLocation)
	if err != nil {
		log.Fatalf("opening database: %v", err)
	}
	defer db.Close()

	songs, err := rekordbox.GetPlaylistTracks(db, playlistName)
	if err != nil {
		log.Fatalf("getting playlist songs: %v", err)
	}

	// Print the songs for demonstration purposes.
	for _, song := range songs {
		fmt.Printf("%s - %s\n", song.Artist, song.Title)
	}
}
