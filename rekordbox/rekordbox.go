package rekordbox

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mutecomm/go-sqlcipher/v4"
	"github.com/r-medina/rdbs"
)

const DBKey = "402fd482c38817c35ffa8ffb8c7d93143b749e7d315df7a81732a1ff43608497"

// FullTrack represents complete track metadata.
type FullTrack struct {
	ID          string
	Title       string
	Artist      string
	Album       string
	AlbumArtist string
	Genre       string
	Label       string
	Year        int
	TrackNumber int
	DiscNumber  int
	BPM         int
	Length      int // In milliseconds
	Key         string
	Rating      int
	ISRC        string
	FileType    string
	DateCreated time.Time
}

// FullPlaylist represents a playlist with full hierarchy context.
type FullPlaylist struct {
	ID          string
	Name        string
	ParentID    string
	ParentName  string
	Seq         int
	Attribute   int
	ImagePath   string
	DateCreated time.Time
	Path        []string // Full hierarchy path
	Tracks      []FullTrack
	Children    []*FullPlaylist
}

// PlaylistNode represents a node in the playlist hierarchy.
type PlaylistNode struct {
	Playlist *FullPlaylist
	Children []*PlaylistNode
}

// Playlist represents basic playlist information.
type Playlist struct {
	ID   string
	Name string
}

// PlaylistInfo represents playlist metadata with parent information.
type PlaylistInfo struct {
	ID         string
	Name       string
	ParentID   string
	ParentName string
	Seq        string
}

// OpenDB opens a read-only SQLite database connection with encryption.
func OpenDB(file string) (*sql.DB, error) {
	dsn := fmt.Sprintf(
		"file:%s?_pragma_key=%s&_pragma_cipher_compatibility=4&mode=ro",
		file,
		url.QueryEscape(DBKey),
	)
	return sql.Open("sqlite3", dsn)
}

// Option defines a configuration function for DB initialization.
type Option func(*DB)

// WithDBLocation sets the database file path.
func WithDBLocation(path string) Option {
	return func(db *DB) {
		db.path = path
	}
}

// DB represents a Rekordbox database connection.
type DB struct {
	path  string
	sqlDB *sql.DB
}

// New creates a new DB instance with the provided options.
func New(opts ...Option) (*DB, error) {
	db := new(DB)

	for _, opt := range opts {
		opt(db)
	}

	if db.path == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		db.path = filepath.Join(homeDir, "Library/Pioneer/rekordbox/master.db")
	}

	sqlDB, err := OpenDB(db.path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database at %s: %w", db.path, err)
	}
	db.sqlDB = sqlDB

	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.sqlDB.Close()
}

// GetFullTrackInfo retrieves complete track metadata by content ID.
func (db *DB) GetFullTrackInfo(contentID string) (*FullTrack, error) {
	query := `
		SELECT
			c.ID,
			c.Title,
			c.TrackNo,
			c.DiscNo,
			c.BPM,
			c.Length,
			c.Rating,
			c.ReleaseYear,
			c.FileType,
			c.DateCreated,
			c.ISRC,
			COALESCE(a.Name, '') AS Artist,
			COALESCE(al.Name, '') AS Album,
			COALESCE(aa.Name, '') AS AlbumArtist,
			COALESCE(g.Name, '') AS Genre,
			COALESCE(l.Name, '') AS Label,
			COALESCE(k.ScaleName, '') AS KeyName
		FROM djmdContent c
		LEFT JOIN djmdArtist a ON c.ArtistID = a.ID
		LEFT JOIN djmdAlbum al ON c.AlbumID = al.ID
		LEFT JOIN djmdArtist aa ON al.AlbumArtistID = aa.ID
		LEFT JOIN djmdGenre g ON c.GenreID = g.ID
		LEFT JOIN djmdLabel l ON c.LabelID = l.ID
		LEFT JOIN djmdKey k ON c.KeyID = k.ID
		WHERE c.ID = ? AND c.rb_local_deleted = 0`

	var track FullTrack
	var dateStr string

	err := db.sqlDB.QueryRow(query, contentID).Scan(
		&track.ID,
		&track.Title,
		&track.TrackNumber,
		&track.DiscNumber,
		&track.BPM,
		&track.Length,
		&track.Rating,
		&track.Year,
		&track.FileType,
		&dateStr,
		&track.ISRC,
		&track.Artist,
		&track.Album,
		&track.AlbumArtist,
		&track.Genre,
		&track.Label,
		&track.Key,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get track info for ID %s: %w", contentID, err)
	}

	if dateStr != "" {
		if parsed, err := time.Parse("2006-01-02 15:04:05", dateStr); err == nil {
			track.DateCreated = parsed
		}
	}

	return &track, nil
}

// GetPlaylistTracksDetailed retrieves all tracks in a playlist with full metadata.
func (db *DB) GetPlaylistTracksDetailed(playlistID string) ([]FullTrack, error) {
	query := `
		SELECT
			c.ID,
			c.Title,
			c.TrackNo,
			c.DiscNo,
			c.BPM,
			c.Length,
			c.Rating,
			c.ReleaseYear,
			c.FileType,
			c.DateCreated,
			c.ISRC,
			COALESCE(a.Name, '') AS Artist,
			COALESCE(al.Name, '') AS Album,
			COALESCE(aa.Name, '') AS AlbumArtist,
			COALESCE(g.Name, '') AS Genre,
			COALESCE(l.Name, '') AS Label,
			COALESCE(k.ScaleName, '') AS KeyName,
			sp.TrackNo AS PlaylistTrackNo
		FROM djmdSongPlaylist sp
		JOIN djmdContent c ON sp.ContentID = c.ID
		LEFT JOIN djmdArtist a ON c.ArtistID = a.ID
		LEFT JOIN djmdAlbum al ON c.AlbumID = al.ID
		LEFT JOIN djmdArtist aa ON al.AlbumArtistID = aa.ID
		LEFT JOIN djmdGenre g ON c.GenreID = g.ID
		LEFT JOIN djmdLabel l ON c.LabelID = l.ID
		LEFT JOIN djmdKey k ON c.KeyID = k.ID
		WHERE sp.PlaylistID = ? AND sp.rb_local_deleted = 0 AND c.rb_local_deleted = 0
		ORDER BY sp.TrackNo`

	rows, err := db.sqlDB.Query(query, playlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tracks for playlist %s: %w", playlistID, err)
	}
	defer rows.Close()

	var tracks []FullTrack
	for rows.Next() {
		var track FullTrack
		var dateStr string
		var playlistTrackNo int

		err := rows.Scan(
			&track.ID,
			&track.Title,
			&track.TrackNumber,
			&track.DiscNumber,
			&track.BPM,
			&track.Length,
			&track.Rating,
			&track.Year,
			&track.FileType,
			&dateStr,
			&track.ISRC,
			&track.Artist,
			&track.Album,
			&track.AlbumArtist,
			&track.Genre,
			&track.Label,
			&track.Key,
			&playlistTrackNo,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan track row: %w", err)
		}

		if dateStr != "" {
			if parsed, err := time.Parse("2006-01-02 15:04:05", dateStr); err == nil {
				track.DateCreated = parsed
			}
		}

		tracks = append(tracks, track)
	}

	return tracks, rows.Err()
}

// GetPlaylistHierarchy retrieves the complete playlist hierarchy.
func (db *DB) GetPlaylistHierarchy() (*PlaylistNode, error) {
	query := `
		SELECT
			p.ID,
			p.Name,
			p.ParentID,
			p.Seq,
			p.Attribute,
			COALESCE(p.ImagePath, '') AS ImagePath,
			p.created_at,
			COALESCE(parent.Name, '') AS ParentName
		FROM djmdPlaylist p
		LEFT JOIN djmdPlaylist parent ON p.ParentID = parent.ID
		WHERE p.rb_local_deleted = 0
		ORDER BY
			CASE
				WHEN p.ParentID IS NULL OR p.ParentID = '' OR p.ParentID = 'root' THEN 0
				ELSE 1
			END,
			p.ParentID,
			p.Seq,
			p.Name`

	rows, err := db.sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get playlist hierarchy: %w", err)
	}
	defer rows.Close()

	playlists := make(map[string]*FullPlaylist)
	for rows.Next() {
		var playlist FullPlaylist
		var parentID sql.NullString
		var dateStr string

		err := rows.Scan(
			&playlist.ID,
			&playlist.Name,
			&parentID,
			&playlist.Seq,
			&playlist.Attribute,
			&playlist.ImagePath,
			&dateStr,
			&playlist.ParentName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan playlist row: %w", err)
		}

		if parentID.Valid {
			playlist.ParentID = parentID.String
		}

		if dateStr != "" {
			if parsed, err := time.Parse("2006-01-02 15:04:05", dateStr); err == nil {
				playlist.DateCreated = parsed
			}
		}

		playlist.Children = make([]*FullPlaylist, 0)
		playlists[playlist.ID] = &playlist
	}

	root := &PlaylistNode{
		Children: make([]*PlaylistNode, 0),
	}

	nodes := make(map[string]*PlaylistNode)
	for id, playlist := range playlists {
		nodes[id] = &PlaylistNode{
			Playlist: playlist,
			Children: make([]*PlaylistNode, 0),
		}
	}

	// Build hierarchy by adding children to their parents
	for id, playlist := range playlists {
		node := nodes[id]
		if playlist.ParentID == "" || playlist.ParentID == "root" {
			root.Children = append(root.Children, node)
		} else if parentNode, exists := nodes[playlist.ParentID]; exists {
			parentNode.Children = append(parentNode.Children, node)
			// Also add to the FullPlaylist children
			parentNode.Playlist.Children = append(parentNode.Playlist.Children, playlist)
		}
	}

	var buildPaths func(node *PlaylistNode, path []string)
	buildPaths = func(node *PlaylistNode, path []string) {
		if node.Playlist != nil {
			node.Playlist.Path = make([]string, len(path))
			copy(node.Playlist.Path, path)
		}
		for _, child := range node.Children {
			childPath := append(path, child.Playlist.Name)
			buildPaths(child, childPath)
		}
	}
	buildPaths(root, []string{})

	return root, rows.Err()
}

// GetFullPlaylist retrieves a playlist with all its tracks and metadata.
func (db *DB) GetFullPlaylist(playlistID string) (*FullPlaylist, error) {
	query := `
		SELECT
			p.ID,
			p.Name,
			p.ParentID,
			p.Seq,
			p.Attribute,
			COALESCE(p.ImagePath, '') AS ImagePath,
			p.created_at,
			COALESCE(parent.Name, '') AS ParentName
		FROM djmdPlaylist p
		LEFT JOIN djmdPlaylist parent ON p.ParentID = parent.ID
		WHERE p.ID = ? AND p.rb_local_deleted = 0`

	var playlist FullPlaylist
	var parentID sql.NullString
	var dateStr string

	err := db.sqlDB.QueryRow(query, playlistID).Scan(
		&playlist.ID,
		&playlist.Name,
		&parentID,
		&playlist.Seq,
		&playlist.Attribute,
		&playlist.ImagePath,
		&dateStr,
		&playlist.ParentName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get playlist %s: %w", playlistID, err)
	}

	if parentID.Valid {
		playlist.ParentID = parentID.String
	}

	if dateStr != "" {
		if parsed, err := time.Parse("2006-01-02 15:04:05", dateStr); err == nil {
			playlist.DateCreated = parsed
		}
	}

	tracks, err := db.GetPlaylistTracksDetailed(playlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to get tracks for playlist %s: %w", playlistID, err)
	}
	playlist.Tracks = tracks
	playlist.Children = make([]*FullPlaylist, 0)

	return &playlist, nil
}

// GetPlaylistsByParent retrieves playlists by parent ID for folder structure syncing.
func (db *DB) GetPlaylistsByParent(parentID string) ([]FullPlaylist, error) {
	var query string
	var args []interface{}

	if parentID == "" || parentID == "root" {
		query = `
			SELECT
				p.ID,
				p.Name,
				p.ParentID,
				p.Seq,
				p.Attribute,
				COALESCE(p.ImagePath, '') AS ImagePath,
				p.created_at,
				'' AS ParentName
			FROM djmdPlaylist p
			WHERE (p.ParentID IS NULL OR p.ParentID = '' OR p.ParentID = 'root') AND p.rb_local_deleted = 0
			ORDER BY p.Seq`
	} else {
		query = `
			SELECT
				p.ID,
				p.Name,
				p.ParentID,
				p.Seq,
				p.Attribute,
				COALESCE(p.ImagePath, '') AS ImagePath,
				p.created_at,
				COALESCE(parent.Name, '') AS ParentName
			FROM djmdPlaylist p
			LEFT JOIN djmdPlaylist parent ON p.ParentID = parent.ID
			WHERE p.ParentID = ? AND p.rb_local_deleted = 0
			ORDER BY p.Seq`
		args = append(args, parentID)
	}

	rows, err := db.sqlDB.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to get playlists by parent %s: %w", parentID, err)
	}
	defer rows.Close()

	var playlists []FullPlaylist
	for rows.Next() {
		var playlist FullPlaylist
		var parentIDNull sql.NullString
		var dateStr string

		err := rows.Scan(
			&playlist.ID,
			&playlist.Name,
			&parentIDNull,
			&playlist.Seq,
			&playlist.Attribute,
			&playlist.ImagePath,
			&dateStr,
			&playlist.ParentName,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan playlist row: %w", err)
		}

		if parentIDNull.Valid {
			playlist.ParentID = parentIDNull.String
		}

		if dateStr != "" {
			if parsed, err := time.Parse("2006-01-02 15:04:05", dateStr); err == nil {
				playlist.DateCreated = parsed
			}
		}

		playlist.Children = make([]*FullPlaylist, 0)
		playlists = append(playlists, playlist)
	}

	return playlists, rows.Err()
}

// GetPlaylistTrackCounts returns track counts per playlist for sync verification.
func (db *DB) GetPlaylistTrackCounts() (map[string]int, error) {
	query := `
		SELECT
			sp.PlaylistID,
			COUNT(*) AS TrackCount
		FROM djmdSongPlaylist sp
		WHERE sp.rb_local_deleted = 0
		GROUP BY sp.PlaylistID`

	rows, err := db.sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get track counts: %w", err)
	}
	defer rows.Close()

	counts := make(map[string]int)
	for rows.Next() {
		var playlistID string
		var count int
		if err := rows.Scan(&playlistID, &count); err != nil {
			return nil, fmt.Errorf("failed to scan count row: %w", err)
		}
		counts[playlistID] = count
	}

	return counts, rows.Err()
}

// GetPlaylistInfo retrieves playlist metadata by name.
func (db *DB) GetPlaylistInfo(name string) ([]PlaylistInfo, error) {
	query := `
		SELECT
			p.ID,
			p.Name,
			p.ParentID,
			p.Seq,
			parent.Name AS ParentName
		FROM djmdPlaylist p
		LEFT JOIN djmdPlaylist parent ON p.ParentID = parent.ID
		WHERE p.Name = ?
		ORDER BY p.ParentID DESC, p.Seq DESC`

	rows, err := db.sqlDB.Query(query, name)
	if err != nil {
		return nil, fmt.Errorf("failed to query playlist '%s': %w", name, err)
	}
	defer rows.Close()

	var playlists []PlaylistInfo
	for rows.Next() {
		var playlist PlaylistInfo
		if err := rows.Scan(&playlist.ID, &playlist.Name, &playlist.ParentID, &playlist.Seq, &playlist.ParentName); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		playlists = append(playlists, playlist)
	}

	return playlists, rows.Err()
}

// GetPlaylistTracks retrieves basic track information for a playlist.
func (db *DB) GetPlaylistTracks(playlistID string) ([]rdbs.Track, error) {
	query := `
		SELECT
			c.Title,
			a.Name
		FROM djmdSongPlaylist sp
		JOIN djmdContent c ON sp.ContentID = c.ID
		JOIN djmdArtist a ON c.ArtistID = a.ID
		WHERE sp.PlaylistID = ?
		ORDER BY sp.TrackNo`

	rows, err := db.sqlDB.Query(query, playlistID)
	if err != nil {
		return nil, fmt.Errorf("failed to query tracks for playlist '%s': %w", playlistID, err)
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

	return tracks, rows.Err()
}

// GetAllPlaylists retrieves all playlists ordered by creation date.
func (db *DB) GetAllPlaylists() ([]Playlist, error) {
	query := `
		SELECT
			ID,
			Name
		FROM djmdPlaylist
		ORDER BY created_at DESC`

	rows, err := db.sqlDB.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query playlists: %w", err)
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

	return playlists, rows.Err()
}

// DumpDatabaseSchema prints the database schema for tables, indexes, views, and triggers.
func (db *DB) DumpDatabaseSchema() error {
	query := `
		SELECT
			type,
			name,
			sql
		FROM sqlite_master
		WHERE type IN ('table', 'index', 'view', 'trigger')`

	rows, err := db.sqlDB.Query(query)
	if err != nil {
		return fmt.Errorf("failed to query schema: %w", err)
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

	return rows.Err()
}
