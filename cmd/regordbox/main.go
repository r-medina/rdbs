package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"syscall"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"github.com/zmb3/spotify"
	"golang.org/x/term"

	"github.com/r-medina/rdbs"
	"github.com/r-medina/rdbs/rekordbox"
)

// Config holds all configuration for the CLI
type Config struct {
	DBLocation          string
	SpotifyClientID     string
	SpotifySecret       string
	SpotifyPlaylistName string
	RekordboxPlaylist   string
}

var config Config

var (
	rootCmd = &cobra.Command{
		Use:   "rdbs",
		Short: "Rekordbox playlist management tool",
		Long:  "A CLI tool for managing Rekordbox playlists and syncing them to Spotify",
	}
	selectCmd = &cobra.Command{
		Use:   "select",
		Short: "Select a playlist and display its tracks",
		Long:  "Interactively select a Rekordbox playlist and display all tracks in it",
		Run:   runSelect,
	}
	treeCmd = &cobra.Command{
		Use:   "tree",
		Short: "Print the Rekordbox playlist directory tree",
		Long:  "Display the complete Rekordbox playlist hierarchy as a directory tree",
		Run:   runTree,
	}
	spotifyCmd = &cobra.Command{
		Use:   "spotify",
		Short: "Sync a Rekordbox playlist to Spotify",
		Long:  "Create or update a Spotify playlist with tracks from a Rekordbox playlist",
		Run:   runSpotify,
	}
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	setupFlags()
	setupCommands()

	failIfError("Command execution faile", rootCmd.Execute())
}

func setupFlags() {
	// Global flags
	rootCmd.PersistentFlags().StringVar(&config.DBLocation, "db", "",
		"Path to the Rekordbox database file (default: system default)")

	// Spotify command flags
	spotifyCmd.Flags().StringVar(&config.SpotifyClientID, "spotify-client-id", "",
		"Spotify client ID (required)")
	spotifyCmd.MarkFlagRequired("spotify-client-id")

	spotifyCmd.Flags().StringVar(&config.SpotifySecret, "spotify-secret", "",
		"Spotify client secret (will prompt if not provided)")

	spotifyCmd.Flags().StringVar(&config.SpotifyPlaylistName, "spotify-playlist-name", "",
		"Name of Spotify playlist (will prompt if not provided)")

	spotifyCmd.Flags().StringVar(&config.RekordboxPlaylist, "rekordbox-playlist-name", "",
		"Name of Rekordbox playlist (will prompt if not provided)")
}

func setupCommands() {
	rootCmd.AddCommand(selectCmd)
	rootCmd.AddCommand(treeCmd)
	rootCmd.AddCommand(spotifyCmd)
}

func runSelect(cmd *cobra.Command, args []string) {
	db := mustInitializeDB()
	defer db.Close()

	playlist, pathName := mustSelectRekordboxPlaylist(db)
	tracks := mustGetPlaylistTracks(db, playlist.ID)

	printTrackList(tracks, pathName)
}

func runTree(cmd *cobra.Command, args []string) {
	db := mustInitializeDB()
	defer db.Close()

	hierarchy := mustGetPlaylistHierarchy(db)
	printDirectoryTree(hierarchy)
}

func runSpotify(cmd *cobra.Command, args []string) {
	db := mustInitializeDB()
	defer db.Close()

	// Get Spotify credentials and authenticate
	ensureSpotifySecret()
	spotifyClient := mustAuthenticateSpotify()

	// Get or create Spotify playlist
	spotifyUser := mustGetCurrentSpotifyUser(spotifyClient)
	spotifyPlaylistID := mustGetOrCreateSpotifyPlaylist(spotifyClient, spotifyUser.ID)

	// Get Rekordbox playlist and tracks
	rekordboxPlaylistID := mustSelectRekordboxPlaylistID(db)
	tracks := mustGetPlaylistTracks(db, rekordboxPlaylistID)

	// Sync to Spotify
	syncTracksToSpotify(spotifyClient, spotifyPlaylistID, tracks)
}

// Database operations
func mustInitializeDB() *rekordbox.DB {
	var db *rekordbox.DB
	var err error

	if config.DBLocation != "" {
		log.Printf("Using database: %s", config.DBLocation)
		db, err = rekordbox.New(rekordbox.WithDBLocation(config.DBLocation))
	} else {
		log.Printf("Using system default database location")
		db, err = rekordbox.New()
	}

	failIfError("Failed to initialize database", err)

	return db
}

func mustGetPlaylistHierarchy(db *rekordbox.DB) *rekordbox.PlaylistNode {
	hierarchy, err := db.GetPlaylistHierarchy()
	failIfError("Failed to get playlist hierarchy", err)
	return hierarchy
}

func mustGetPlaylistTracks(db *rekordbox.DB, playlistID string) []rdbs.Track {
	tracks, err := db.GetPlaylistTracks(playlistID)
	failIfError("Failed to get playlist tracks", err)
	return tracks
}

// Playlist selection
func mustSelectRekordboxPlaylist(db *rekordbox.DB) (*rekordbox.FullPlaylist, string) {
	hierarchy := mustGetPlaylistHierarchy(db)
	playlists := collectPlaylistsWithTracks(hierarchy, db)

	if len(playlists) == 0 {
		log.Fatal("No playlists with tracks found")
	}

	return selectFromPlaylistCollection(playlists)
}

func mustSelectRekordboxPlaylistID(db *rekordbox.DB) string {
	if config.RekordboxPlaylist == "" {
		playlist, _ := mustSelectRekordboxPlaylist(db)
		return playlist.ID
	}

	return findPlaylistIDByName(db, config.RekordboxPlaylist)
}

func findPlaylistIDByName(db *rekordbox.DB, name string) string {
	playlists, err := db.GetPlaylistInfo(name)
	failIfError(fmt.Sprintf("Failed to get playlist %q info", name), err)

	switch len(playlists) {
	case 0:
		log.Fatalf("No playlist found with name '%s'", name)
	case 1:
		return playlists[0].ID
	default:
		return selectFromMultipleMatches(db, playlists)
	}
	return ""
}

func selectFromMultipleMatches(db *rekordbox.DB, playlists []rekordbox.PlaylistInfo) string {
	formatted := make([]string, len(playlists))
	for i, p := range playlists {
		full, err := db.GetFullPlaylist(p.ID)
		failIfError("Failed to get full playlist info", err)
		formatted[i] = strings.Join(full.Path, " > ")
	}

	prompt := promptui.Select{
		Label:             "Multiple playlists found, select one",
		Items:             formatted,
		Stdout:            os.Stderr,
		StartInSearchMode: true,
	}

	i, _, err := prompt.Run()
	failIfError("Failed to select playlist", err)

	return playlists[i].ID
}

func collectPlaylistsWithTracks(node *rekordbox.PlaylistNode, db *rekordbox.DB) []*rekordbox.PlaylistNode {
	var playlists []*rekordbox.PlaylistNode
	collectPlaylistsWithTracksRecursive(node, &playlists, db)
	return playlists
}

func collectPlaylistsWithTracksRecursive(node *rekordbox.PlaylistNode, playlists *[]*rekordbox.PlaylistNode, db *rekordbox.DB) {
	if node.Playlist != nil {
		tracks, err := db.GetPlaylistTracks(node.Playlist.ID)
		if err != nil {
			log.Printf("Warning: could not get tracks for playlist %s: %v", node.Playlist.Name, err)
		} else if len(tracks) > 0 {
			*playlists = append(*playlists, node)
		}
	}

	for _, child := range node.Children {
		collectPlaylistsWithTracksRecursive(child, playlists, db)
	}
}

func selectFromPlaylistCollection(playlists []*rekordbox.PlaylistNode) (*rekordbox.FullPlaylist, string) {
	formatted := make([]string, len(playlists))
	for i, p := range playlists {
		if p.Playlist != nil {
			formatted[i] = strings.Join(p.Playlist.Path, " > ")
		}
	}

	terminalHeight := getTerminalHeight()
	adjustedHeight := max(terminalHeight-4, 5) // Account for prompt label and padding

	searcher := func(input string, index int) bool {
		if index >= len(formatted) {
			return false
		}
		return fuzzy.MatchFold(input, formatted[index])
	}

	prompt := promptui.Select{
		Label:             "Select a playlist (type to search)",
		Items:             formatted,
		Size:              adjustedHeight,
		Stdout:            os.Stderr,
		Searcher:          searcher,
		StartInSearchMode: true,
	}

	i, _, err := prompt.Run()
	failIfError("Failed to select playlist", err)

	return playlists[i].Playlist, formatted[i]
}

// Spotify operations
func ensureSpotifySecret() {
	if config.SpotifySecret != "" {
		return
	}

	fmt.Print("Enter your Spotify client secret: ")
	secretBytes, err := term.ReadPassword(int(syscall.Stdin))
	failIfError("Failed to read Spotify secret", err)
	config.SpotifySecret = string(secretBytes)
	fmt.Println() // newline after password input
}

func mustAuthenticateSpotify() *spotify.Client {
	log.Println("Opening browser to authenticate with Spotify...")

	client, err := rdbs.SpotifyOAuthClient(config.SpotifyClientID, config.SpotifySecret)
	failIfError("Spotify OAuth failed: %v", err)

	return client
}

func mustGetCurrentSpotifyUser(client *spotify.Client) *spotify.PrivateUser {
	user, err := client.CurrentUser()
	failIfError("Failed to get current Spotify user", err)

	log.Printf("Authenticated as: %s", user.DisplayName)
	return user
}

func mustGetOrCreateSpotifyPlaylist(client *spotify.Client, userID string) spotify.ID {
	ensureSpotifyPlaylistName()

	playlists := mustGetUserPlaylists(client, userID)
	matches := findMatchingPlaylists(playlists, config.SpotifyPlaylistName)

	switch len(matches) {
	case 0:
		return createNewSpotifyPlaylist(client, userID)
	case 1:
		log.Printf("Using existing playlist: %s", matches[0].p.Name)
		return matches[0].p.ID
	default:
		return selectFromMultipleSpotifyPlaylists(matches)
	}
}

func ensureSpotifyPlaylistName() {
	if config.SpotifyPlaylistName != "" {
		return
	}

	prompt := promptui.Prompt{
		Label: "Enter name for Spotify playlist",
	}
	var err error
	config.SpotifyPlaylistName, err = prompt.Run()
	failIfError("Failed to get Spotify playlist name", err)
}

func mustGetUserPlaylists(client *spotify.Client, userID string) *spotify.SimplePlaylistPage {
	playlists, err := client.GetPlaylistsForUser(userID)
	failIfError("Failed to get user playlists", err)
	return playlists
}

type playlistMatch struct {
	index int
	p     spotify.SimplePlaylist
}

func findMatchingPlaylists(playlists *spotify.SimplePlaylistPage, name string) []playlistMatch {
	var matches []playlistMatch
	for i, p := range playlists.Playlists {
		if p.Name == name {
			matches = append(matches, playlistMatch{index: i, p: p})
		}
	}
	return matches
}

func createNewSpotifyPlaylist(client *spotify.Client, userID string) spotify.ID {
	log.Printf("Creating new playlist: %s", config.SpotifyPlaylistName)

	playlist, err := client.CreatePlaylistForUser(
		userID,
		config.SpotifyPlaylistName,
		"Exported from Rekordbox",
		false, // public
	)
	failIfError("Failed to create playlist", err)

	return playlist.ID
}

func selectFromMultipleSpotifyPlaylists(matches []playlistMatch) spotify.ID {
	formatted := make([]string, len(matches))
	for i, match := range matches {
		formatted[i] = fmt.Sprintf("%s (%d tracks)", match.p.Name, match.p.Tracks.Total)
	}

	prompt := promptui.Select{
		Label:             "Multiple playlists found, select one",
		Items:             formatted,
		Stdout:            os.Stderr,
		StartInSearchMode: true,
	}

	i, _, err := prompt.Run()
	failIfError("Failed to select playlist", err)

	return matches[i].p.ID
}

func syncTracksToSpotify(client *spotify.Client, playlistID spotify.ID, tracks []rdbs.Track) {
	log.Printf("Searching for %d tracks on Spotify...", len(tracks))

	spotifyTracks, err := rdbs.SpotifySearch(client, tracks)
	failIfError("Failed to search tracks on Spotify", err)

	log.Println("Adding tracks to playlist...")
	addedCount := 0
	for _, track := range spotifyTracks {
		if track.ID == "" {
			continue
		}

		_, err := client.AddTracksToPlaylist(playlistID, track.ID)
		if err != nil {
			log.Printf("Failed to add track '%s': %v", track.Name, err)
		} else {
			addedCount++
		}
	}

	log.Printf("Successfully added %d tracks to playlist", addedCount)
}

// Display functions
func printTrackList(tracks []rdbs.Track, playlistName string) {
	fmt.Printf("\nTracks in %s:\n", playlistName)
	fmt.Println(strings.Repeat("=", len(playlistName)+11))

	for i, track := range tracks {
		fmt.Printf("%3d. %s - %s\n", i+1, track.Artist, track.Title)
	}

	fmt.Printf("\nTotal: %d tracks\n", len(tracks))
}

func printDirectoryTree(hierarchy *rekordbox.PlaylistNode) {
	fmt.Println("Rekordbox Playlist Directory Tree:")
	fmt.Println(strings.Repeat("=", 35))
	printDirectoryTreeRecursive(hierarchy, "", true)
}

func printDirectoryTreeRecursive(node *rekordbox.PlaylistNode, prefix string, isLast bool) {
	if node.Playlist != nil {
		connector := "├── "
		if isLast {
			connector = "└── "
		}

		fmt.Printf("%s%s%s\n", prefix, connector, node.Playlist.Name)

		childPrefix := prefix + "│   "
		if isLast {
			childPrefix = prefix + "    "
		}
		printDirectoryChildren(node, childPrefix)
	} else {
		printDirectoryChildren(node, prefix)
	}
}

func printDirectoryChildren(node *rekordbox.PlaylistNode, prefix string) {
	children := make([]*rekordbox.PlaylistNode, len(node.Children))
	copy(children, node.Children)

	// Sort children alphabetically
	sort.Slice(children, func(i, j int) bool {
		return strings.Compare(children[i].Playlist.Name, children[j].Playlist.Name) < 0
	})

	for i, child := range children {
		isLast := i == len(children)-1
		printDirectoryTreeRecursive(child, prefix, isLast)
	}
}

// Utility functions
func getTerminalHeight() int {
	_, height, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		log.Printf("Warning: could not get terminal size, using default: %v", err)
		return 10 // Fallback
	}
	return height
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func failIfError(msg string, err error) {
	if err == nil {
		return
	}
	log.Fatalf("%s: %v", msg, err)
}
