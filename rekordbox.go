package rdbs

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"

	_ "github.com/mutecomm/go-sqlcipher/v4"
)

const DBKey = "402fd482c38817c35ffa8ffb8c7d93143b749e7d315df7a81732a1ff43608497"

type Song struct {
	Title  string
	Artist string
}

func OpenDB(file string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma_key=%s&_pragma_cipher_compatibility=4&mode=ro",
		file,
		url.QueryEscape(DBKey),
	)
	return sql.Open("sqlite3", dsn)
}

func GetPlaylistSongs(db *sql.DB, playlistName string) ([]Song, error) {
	query := `SELECT c.Title, a.Name
        FROM djmdSongPlaylist sp
        JOIN djmdContent c ON sp.ContentID = c.ID
        JOIN djmdArtist a ON c.ArtistID = a.ID
        JOIN djmdPlaylist p ON sp.PlaylistID = p.ID
        WHERE p.Name = ?`

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

	return songs, rows.Err()
}
