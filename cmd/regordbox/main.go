package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"os"

	_ "github.com/mutecomm/go-sqlcipher/v4"
)

const DBKey = "402fd482c38817c35ffa8ffb8c7d93143b749e7d315df7a81732a1ff43608497"

// Song represents a song with a title and an artist.
type Song struct {
	Title  string
	Artist string
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if len(os.Args) < 3 {
		log.Fatalf("Usage: %s <db_location> <playlist_name>", os.Args[0])
	}

	dbLocation := os.Args[1]
	playlistName := os.Args[2]

	// Open the database connection
	dsn := fmt.Sprintf("file:%s?_pragma_key=%s&_pragma_cipher_compatibility=4", dbLocation, url.QueryEscape(DBKey))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	query := `
        SELECT c.Title, a.Name AS Artist
        FROM djmdSongPlaylist sp
        JOIN djmdContent c ON sp.ContentID = c.ID
        JOIN djmdArtist a ON c.ArtistID = a.ID
        JOIN djmdPlaylist p ON sp.PlaylistID = p.ID
        WHERE p.Name = ?
    `

	rows, err := db.Query(query, playlistName)
	if err != nil {
		log.Fatalf("Failed to execute query: %v", err)
	}
	defer rows.Close()

	var songs []Song

	for rows.Next() {
		var song Song
		if err := rows.Scan(&song.Title, &song.Artist); err != nil {
			log.Fatalf("Failed to scan row: %v", err)
		}
		songs = append(songs, song)
	}

	if err := rows.Err(); err != nil {
		log.Fatalf("Error reading rows: %v", err)
	}

	// Print the songs for demonstration purposes.
	for _, song := range songs {
		fmt.Printf("%s - %s\n", song.Artist, song.Title)
	}
}
