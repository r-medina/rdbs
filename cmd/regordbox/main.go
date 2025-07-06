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

var (
	dbLocation string

	rootCmd = &cobra.Command{
		Use:   "rdbs",
		Short: "Rekordbox playlist management tool",
	}
	selectCmd = &cobra.Command{
		Use:   "select",
		Short: "Select a playlist and display its tracks",
		Run:   runSelect,
	}
	treeCmd = &cobra.Command{
		Use:   "tree",
		Short: "Print the Rekordbox playlist directory tree",
		Run:   runTree,
	}
	spotifyCmd = &cobra.Command{
		Use:   "spotify",
		Short: "Sync a Rekordbox playlist into Spotify",
		Run:   runSpotify,
	}

	spotifyClientID     string
	spotifySecret       string
	spotifyPlaylistName string
	rekordboxPlaylist   string
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Define global flag
	rootCmd.PersistentFlags().StringVar(&dbLocation, "db", "", "path to the rekordbox database file (default: system default)")

	// Define local flags
	spotifyCmd.Flags().StringVar(&spotifyClientID, "spotify-client-id", "", "Spotify client ID")
	spotifyCmd.MarkFlagRequired("spotify-client-id")
	spotifyCmd.Flags().StringVar(&spotifySecret, "spotify-secret", "", "Spotify secret")
	spotifyCmd.Flags().StringVar(
		&spotifyPlaylistName,
		"spotify-playlist-name",
		"",
		"Name of spotify playlist - if it already exists, songs are added",
	)
	spotifyCmd.Flags().StringVar(
		&rekordboxPlaylist,
		"rekordbox-playlist-name",
		"",
		"Name of Rekordbox playlist",
	)
	// Add commands
	rootCmd.AddCommand(selectCmd)
	rootCmd.AddCommand(treeCmd)
	rootCmd.AddCommand(spotifyCmd)

	// Execute the root command
	failIfError("executing command", rootCmd.Execute())
}

func runSelect(cmd *cobra.Command, args []string) {
	db, err := initializeDB()
	failIfError("initializing database", err)
	defer db.Close()

	playlist, pathName := selectRekordboxPlaylist(db)

	// Get tracks for the selected playlist
	songs, err := db.GetPlaylistTracks(playlist.ID)
	failIfError("getting playlist songs", err)

	// Print the songs
	fmt.Printf("\nTracks in %s:\n", pathName)
	fmt.Println("==================================")
	for _, song := range songs {
		fmt.Printf("%s - %s\n", song.Artist, song.Title)
	}
}

func runTree(cmd *cobra.Command, args []string) {
	db, err := initializeDB()
	if err != nil {
		log.Fatalf("initializing database: %v", err)
	}
	defer db.Close()

	// Get the complete playlist hierarchy
	hierarchy, err := db.GetPlaylistHierarchy()
	if err != nil {
		log.Fatalf("getting playlist hierarchy: %v", err)
	}

	// Print the directory tree
	fmt.Println("Rekordbox Playlist Directory Tree:")
	fmt.Println("==================================")
	printDirectoryTree(hierarchy, "", true)
}

func runSpotify(cmd *cobra.Command, args []string) {
	db, err := initializeDB()
	failIfError("initializing database", err)
	defer db.Close()

	if spotifySecret == "" {
		fmt.Print("input your Spotify secret key: ")
		secretBytes, err := term.ReadPassword(int(syscall.Stdin))
		failIfError("error reading Spotify secret key", err)
		spotifySecret = string(secretBytes)
		fmt.Println() // newline after secret
	}

	log.Println("opening browser to authenticate with spotify")

	spotifyClient, err := rdbs.SpotifyOAuthClient(spotifyClientID, spotifySecret)
	failIfError("oauth client failed", err)

	spotifyUser, err := spotifyClient.CurrentUser()
	failIfError("could not get current user", err)
	log.Printf("user: %s", spotifyUser.DisplayName)

	if spotifyPlaylistName == "" {
		prompt := promptui.Prompt{Label: "Name to be used for Spotify playlist"}
		spotifyPlaylistName, err = prompt.Run()
		failIfError("selecting Spotify playlist name", err)
	}

	playlists, err := spotifyClient.GetPlaylistsForUser(spotifyUser.ID)
	failIfError("getting user spotify playlists", err)

	var spotifyPlaylistID spotify.ID
	type match struct {
		i int
		p spotify.SimplePlaylist
	}
	candidates := []match{}

	for i, p := range playlists.Playlists {
		if p.Name == spotifyPlaylistName {
			candidates = append(candidates, match{i: i, p: p})
		}
	}
	if len(candidates) > 1 {
		formatted := make([]string, len(candidates))
		for i, p := range candidates {
			formatted[i] = fmt.Sprintf("%s (%d)", p.p.Name, p.p.Tracks.Total)
		}

		// Prompt user to select a playlist with fuzzy search
		prompt := promptui.Select{
			Label:             "Select a playlist",
			Items:             formatted,
			Stdout:            os.Stderr,
			StartInSearchMode: true,
		}
		i, _, err := prompt.Run()
		failIfError("selecting playlist", err)
		spotifyPlaylistID = candidates[i].p.ID
	} else {
		playlist, err := spotifyClient.CreatePlaylistForUser(spotifyUser.ID, spotifyPlaylistName, "exported with regordbox", false)
		failIfError("creating playlist", err)
		spotifyPlaylistID = playlist.ID
	}

	// get tracks from rekordbox

	var rekordboxPlaylistID string
	if rekordboxPlaylist == "" {
		fmt.Println("here")
		playlist, _ := selectRekordboxPlaylist(db)
		rekordboxPlaylistID = playlist.ID
		// Get tracks for the selected playlist
	} else {
		playlists, err := db.GetPlaylistInfo(rekordboxPlaylist)
		if len(playlists) == 1 {
			rekordboxPlaylistID = playlists[0].ID
		} else {
			formatted := make([]string, len(playlists))
			for i, p := range playlists {
				full, err := db.GetFullPlaylist(p.ID)
				failIfError("getting full playlist", err)

				formatted[i] = strings.Join(full.Path, " > ")
			}
			failIfError("getting playlist info", err)
			if len(playlists) > 2 {
				prompt := promptui.Select{
					Label:  "Select a playlist",
					Items:  playlists,
					Stdout: os.Stderr,
				}
				i, _, err := prompt.Run()
				failIfError("searching for rekordbox playlist", err)
				rekordboxPlaylistID = playlists[i].ID
			}
		}
	}

	tracks, err := db.GetPlaylistTracks(rekordboxPlaylistID)
	failIfError("getting playlist songs", err)

	spotifyTracks, err := rdbs.SpotifySearch(spotifyClient, tracks)
	failIfError("searching on spotify", err)
	log.Println("adding songs to playlist")
	for _, spotifyTrack := range spotifyTracks {
		if spotifyTrack.ID == "" {
			continue
		}
		_, err = spotifyClient.AddTracksToPlaylist(spotifyPlaylistID, spotifyTrack.ID)
		if err != nil {
			log.Printf("could not add track %q to playlist: %+v", spotifyTrack.Name, err)
		}
	}
}

func initializeDB() (*rekordbox.DB, error) {
	var db *rekordbox.DB
	var err error
	if dbLocation != "" {
		log.Printf("using database: %s", dbLocation)
		db, err = rekordbox.New(
			rekordbox.WithDBLocation(dbLocation),
		)
	} else {
		log.Printf("using system default database location")
		db, err = rekordbox.New()
	}
	return db, err
}

func selectRekordboxPlaylist(db *rekordbox.DB) (*rekordbox.FullPlaylist, string) {
	hierarchy, err := db.GetPlaylistHierarchy()
	failIfError("getting playlist hierarchy", err)

	// Collect all playlists with tracks for selection
	var playlists []*rekordbox.PlaylistNode
	collectPlaylistsWithTracks(hierarchy, &playlists, db)

	// Format playlist names with full path for selection
	formatted := make([]string, len(playlists))
	for i, p := range playlists {
		if p.Playlist != nil {
			formatted[i] = strings.Join(p.Playlist.Path, " > ")
		}
	}

	// Get terminal height
	_, termHeight, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		log.Printf("warning: could not get terminal size, using default select size: %v", err)
		termHeight = 10 // Fallback to a reasonable default
	}

	// Adjust termHeight to account for prompt label and some padding
	adjustedHeight := max(termHeight-4, 5)
	searcher := func(input string, index int) bool {
		if index >= len(formatted) {
			return false
		}
		return fuzzy.MatchFold(input, formatted[index])
	}

	// Prompt user to select a playlist with fuzzy search
	prompt := promptui.Select{
		Label:             "Select a playlist (type to search)",
		Items:             formatted,
		Size:              adjustedHeight,
		Stdout:            os.Stderr,
		Searcher:          searcher,
		StartInSearchMode: true,
	}
	i, _, err := prompt.Run()
	failIfError("selecting playlist", err)

	// Get tracks for the selected playlist
	return playlists[i].Playlist, formatted[i]
}

// printDirectoryTree recursively prints the playlist hierarchy as a tree
func printDirectoryTree(node *rekordbox.PlaylistNode, prefix string, isLast bool) {
	if node.Playlist != nil {
		var connector string
		if isLast {
			connector = "└── "
		} else {
			connector = "├── "
		}
		fmt.Printf("%s%s%s\n", prefix, connector, node.Playlist.Name)
		var childPrefix string
		if isLast {
			childPrefix = prefix + "    "
		} else {
			childPrefix = prefix + "│   "
		}
		printChildren(node, childPrefix)
	} else {
		printChildren(node, prefix)
	}
}

// printChildren prints all child nodes
func printChildren(node *rekordbox.PlaylistNode, prefix string) {
	var children []*rekordbox.PlaylistNode
	children = append(children, node.Children...)
	sort.Slice(children, func(i, j int) bool {
		return strings.Compare(children[i].Playlist.Name, children[j].Playlist.Name) < 0
	})
	for i, child := range children {
		isLast := i == len(children)-1
		printDirectoryTree(child, prefix, isLast)
	}
}

// collectPlaylistsWithTracks gathers all playlists with at least one track into a slice
func collectPlaylistsWithTracks(node *rekordbox.PlaylistNode, playlists *[]*rekordbox.PlaylistNode, db *rekordbox.DB) {
	if node.Playlist != nil {
		tracks, err := db.GetPlaylistTracks(node.Playlist.ID)
		if err != nil {
			log.Printf("warning: could not get tracks for playlist %s: %v", node.Playlist.Name, err)
			return
		}
		if len(tracks) > 0 {
			*playlists = append(*playlists, node)
		}
	}
	for _, child := range node.Children {
		collectPlaylistsWithTracks(child, playlists, db)
	}
}

func failIfError(msg string, err error) {
	if err == nil {
		return
	}
	log.Fatalf("%s: %v", msg, err)
}
