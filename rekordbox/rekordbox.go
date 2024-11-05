package rekordbox

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/mutecomm/go-sqlcipher/v4"

	"github.com/r-medina/rdbs"
)

const DBKey = "402fd482c38817c35ffa8ffb8c7d93143b749e7d315df7a81732a1ff43608497"

type Playlist struct {
	ID   string
	Name string
}

func OpenDB(file string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma_key=%s&_pragma_cipher_compatibility=4&mode=ro",
		file,
		url.QueryEscape(DBKey),
	)
	return sql.Open("sqlite3", dsn)
}

func GetPlaylistTracks(db *sql.DB, playlistID string) ([]rdbs.Track, error) {
	query := `SELECT c.Title, a.Name
		FROM djmdSongPlaylist sp
		JOIN djmdContent c ON sp.ContentID = c.ID
		JOIN djmdArtist a ON c.ArtistID = a.ID
		JOIN djmdPlaylist p ON sp.PlaylistID = p.ID
		WHERE p.ID = ?
		ORDER BY sp.TrackNo` // Order by TrackNo to maintain playlist order

	rows, err := db.Query(query, playlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query for playlist '%s': %w", playlistID, err)
	}
	defer rows.Close()

	var tracks []rdbs.Track
	for rows.Next() {
		var track rdbs.Track
		if err := rows.Scan(&track.Title, &track.Artist); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		tracks = append(tracks, track)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return tracks, nil
}

func GetAllPlaylists(db *sql.DB) ([]Playlist, error) {
	query := `SELECT ID, Name FROM djmdPlaylist ORDER BY created_at DESC`

	rows, err := db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to execute playlist query: %w", err)
	}
	defer rows.Close()

	var playlists []Playlist
	for rows.Next() {
		var playlist Playlist
		if err := rows.Scan(&playlist.ID, &playlist.Name); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		playlists = append(playlists, playlist)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating over rows: %w", err)
	}

	return playlists, nil
}

func DumpDatabaseSchema(db *sql.DB) error {
	query := `SELECT type, name, sql FROM sqlite_master WHERE type IN ('table', 'index', 'view', 'trigger')`

	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("failed to execute schema dump query: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var objType, name, sqlStmt sql.NullString
		if err := rows.Scan(&objType, &name, &sqlStmt); err != nil {
			return fmt.Errorf("failed to scan row: %w", err)
		}
		if objType.Valid && name.Valid && sqlStmt.Valid {
			fmt.Printf("-- %s: %s\n%s;\n\n", objType.String, name.String, sqlStmt.String)
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating over rows: %w", err)
	}

	return nil
}
