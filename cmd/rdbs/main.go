package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"os/user"
	"strings"
	"syscall"

	"golang.org/x/term"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"

	"github.com/pkg/errors"
	"github.com/zmb3/spotify"

	"github.com/r-medina/rdbs"
	"github.com/r-medina/rdbs/rekordbox"
)

var (
	dry            bool
	useRekordbox   bool
	uploadAll      bool
	folderName     string
	rekordboxDBFmt = "/Users/%s/Library/Pioneer/rekordbox/master.db"
	manyPlaylists  int
)

func help() {
	fmt.Println(`rdbs [-d] [-r] [-a] [<spotify-playlist-name> <playlist-location>]
	-d	dry run (only search song names - don't make playlist)
	-r	read from rekordbox database instead of file
	-a	upload all rekordbox playlists to spotify
	-n      number of playlists to upload`)
}

func init() {
	flag.BoolVar(&dry, "d", false, "dry run")
	flag.BoolVar(&useRekordbox, "r", false, "use rekordbox")
	flag.BoolVar(&uploadAll, "a", false, "upload all rekordbox playlists to spotify")
	flag.StringVar(&folderName, "f", "rdbs", "optional folder name to group playlists in spotify")
	flag.IntVar(&manyPlaylists, "n", 1, "number of playlists to upload")
}

func main() {
	flag.Parse()

	if flag.NArg() < 2 && !uploadAll {
		help()
		os.Exit(1)
	}

	if dry {
		log.SetFlags(log.Lshortfile)
		log.Println("executing dry run")
	}

	spotifyClientID := os.Getenv("SPOTIFY_ID")
	spotifySecret := os.Getenv("SPOTIFY_SECRET")

	if spotifyClientID == "" {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("input your Spotify client ID: ")
		var err error
		spotifyClientID, err = reader.ReadString('\n')
		failIfError("error reading Spotify client ID", err)
		spotifyClientID = strings.TrimSuffix(spotifyClientID, "\n")
	}

	if spotifySecret == "" {
		fmt.Print("input your Spotify secret key: ")
		secretBytes, err := term.ReadPassword(int(syscall.Stdin))
		failIfError("error reading Spotify secret key", err)
		spotifySecret = string(secretBytes)
		fmt.Println() // newline after secret
	}

	log.Println("opening browser to authenticate with spotify")

	var err error
	spotifyClient, err := rdbs.SpotifyOAuthClient(spotifyClientID, spotifySecret)
	failIfError("oauth client failed", err)

	spotifyUser, err := spotifyClient.CurrentUser()
	failIfError("could not get current user", err)
	log.Printf("user: %s", spotifyUser.DisplayName)

	rekordboxDB := os.Getenv("REKORDBOX_DB")
	if rekordboxDB == "" {
		u, err := user.Current()
		failIfError("getting user for finding rekordbox db", err)
		rekordboxDB = fmt.Sprintf(rekordboxDBFmt, u.Username)
	}

	db, err := rekordbox.OpenDB(rekordboxDB)
	failIfError("opening rekordbox db", err)
	defer db.Close()

	if uploadAll {
		log.Println("uploading all playlists to Spotify - just kidding")
		// playlists, err := rekordbox.GetAllPlaylists(db)
		// failIfError("could not retrieve all playlists", err)
		// for _, playlist := range playlists[:manyPlaylists] {
		// 	log.Printf("loading playlist %q into spotify", playlist.Name)
		// 	tracks, err := rekordbox.GetPlaylistTracks(db, playlist.ID)
		// 	failIfError("reading playlist tracks", err)
		// 	uploadPlaylist(spotifyClient, spotifyUser.ID, playlist.Name, tracks)
		// 	time.Sleep(2 * time.Second)
		// }
	} else {
		playlistName := flag.Args()[0]
		playlistLocation := flag.Args()[1]
		log.Printf("loading %q into spotify as %q", playlistLocation, playlistName)
		var tracks []rdbs.Track
		if !useRekordbox {
			data, err := readData(playlistLocation)
			failIfError("could not read the playlist file: %+v", err)
			tracks, err = listTracks(data)
			failIfError("could not list tracks", err)
		} else {
			playlists, err := rekordbox.GetPlaylistInfo(db, playlistLocation)
			failIfError("getting playlist info", err)
			tracks, err = rekordbox.GetPlaylistTracks(db, playlists[0].ID)
			failIfError("reading playlist tracks", err)
		}
		uploadPlaylist(spotifyClient, spotifyUser.ID, playlistName, tracks)
	}
}

func uploadPlaylist(spotifyClient *spotify.Client, userID, playlistName string, tracks []rdbs.Track) {
	if !dry {
		playlist, err := spotifyClient.CreatePlaylistForUser(userID, fmt.Sprintf("%s/%s", folderName, playlistName), "exported from rekordbox", false)
		failIfError("could not create playlist", err)
		spotifyTracks, err := rdbs.SpotifySearch(spotifyClient, tracks)
		failIfError("searching on spotify", err)
		log.Println("adding songs to playlist")
		for _, spotifyTrack := range spotifyTracks {
			if spotifyTrack.ID == "" {
				continue
			}
			_, err = spotifyClient.AddTracksToPlaylist(playlist.ID, spotifyTrack.ID)
			if err != nil {
				log.Printf("could not add track %q to playlist: %+v", spotifyTrack.Name, err)
			}
		}
	} else {
		for _, track := range tracks {
			fmt.Printf("\t%s - %s\n", track.Artist, track.Title)
		}
	}
}

func readData(fname string) ([][]string, error) {
	original, err := os.Open(fname)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	defer original.Close()

	dec := unicode.UTF16(unicode.LittleEndian, unicode.UseBOM).NewDecoder()
	utf8r := transform.NewReader(original, dec)
	csvr := csv.NewReader(utf8r)

	csvr.Comma = '\t'
	csvr.FieldsPerRecord = -1
	records, err := csvr.ReadAll()
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return records, nil
}

func listTracks(data [][]string) ([]rdbs.Track, error) {
	var tracks []rdbs.Track
	var ia int
	var it int
	for j, field := range data[0] {
		if field == "Artist" {
			ia = j
		} else if field == "Track Title" {
			it = j
		}
	}
	for _, d := range data[1:] {
		if len(d) < 4 {
			continue
		}
		artist := strings.TrimSpace(d[ia])
		title := strings.TrimSpace(d[it])
		tracks = append(tracks, rdbs.Track{Artist: artist, Title: title})
	}
	return tracks, nil
}

func failIfError(msg string, err error) {
	if err == nil {
		return
	}
	log.Fatalf("%s: %v", msg, err)
}
