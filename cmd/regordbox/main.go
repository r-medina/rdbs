package main

import (
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/manifoldco/promptui"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/r-medina/rdbs/rekordbox"
)

var (
	dbLocation string
	rootCmd    = &cobra.Command{
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
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Define global flag
	rootCmd.PersistentFlags().StringVar(&dbLocation, "db", "", "path to the rekordbox database file (default: system default)")

	// Add commands
	rootCmd.AddCommand(selectCmd)
	rootCmd.AddCommand(treeCmd)

	// Execute the root command
	if err := rootCmd.Execute(); err != nil {
		log.Fatalf("executing command: %v", err)
	}
}

func runSelect(cmd *cobra.Command, args []string) {
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
	if err != nil {
		log.Fatalf("selecting playlist: %v", err)
	}

	// Get tracks for the selected playlist
	songs, err := db.GetPlaylistTracks(playlists[i].Playlist.ID)
	if err != nil {
		log.Fatalf("getting playlist songs: %v", err)
	}

	// Print the songs
	fmt.Printf("\nTracks in %s:\n", formatted[i])
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
	for _, child := range node.Children {
		children = append(children, child)
	}
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
