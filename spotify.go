package rdbs

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/skratchdot/open-golang/open"
	"github.com/zmb3/spotify"
)

func SpotifyOAuthClient(clientID, secretKey string) (*spotify.Client, error) {
	auth := spotify.NewAuthenticator("http://localhost:8666/", spotify.ScopePlaylistModifyPrivate)
	auth.SetAuthInfo(clientID, secretKey)

	var client *spotify.Client
	httpDone := make(chan error)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			log.Println(r.Method, r.URL.Path)
			return
		}
		defer close(httpDone)

		token, err := auth.Token("", r)
		if err != nil {
			log.Println("error getting token:", err)
			http.Error(w, "failed to get token", http.StatusNotFound)
			httpDone <- errors.Wrap(err, "failed to get token")
			return
		}
		c := auth.NewClient(token)
		client = &c

		fmt.Fprintf(w, `you may close this webpage`)
	})

	go func() {
		http.ListenAndServe("localhost:8666", nil)
	}()

	// print brower url in case it doesnt open automatically
	fmt.Printf("opening browser to %s\n", auth.AuthURL(""))
	if err := open.Run(auth.AuthURL("")); err != nil {
		return nil, err
	}

	select {
	case err := <-httpDone:
		return client, err
	case <-time.After(120 * time.Second):
		return nil, errors.New("timeout waiting for oauth token")
	}
}

func SpotifySearch(spotifyClient *spotify.Client, tracks []Track) ([]spotify.FullTrack, error) {
	wg := sync.WaitGroup{}
	trackCh := make(chan spotify.FullTrack, len(tracks))
	for _, t := range tracks {
		wg.Add(1)
		go func(track Track) {
			defer wg.Done()
			artist := track.Artist
			title := track.Title

			fmt.Printf("\t%s - %s\n", artist, title)

			// spotify doesnt like the (Original Mix) or (Someone
			// Remix) that dance music uses
			// also doesnt like "feat"

			title = strings.ToLower(title)
			title = strings.ReplaceAll(title, "original mix", "")
			title = strings.ReplaceAll(title, "(", "")
			title = strings.ReplaceAll(title, ")", "")
			title = strings.ReplaceAll(title, "feat.", "")

			end := len(artist)
			if i := strings.Index(artist, "("); i > 0 {
				end = i
			}
			artist = artist[0:end]

			q := fmt.Sprintf("%s %s", artist, title)
			results, err := spotifyClient.Search(q, spotify.SearchTypeTrack)
			if err != nil {
				log.Printf("spotify search failed: %+v", err)
				return
			}

			if results.Tracks != nil {
				if len(results.Tracks.Tracks) == 0 {
					log.Printf("could not find '%s - %s'", artist, title)
					return
				}
				trackCh <- results.Tracks.Tracks[0]
			}
		}(t)
	}

	go func() {
		wg.Wait()
		close(trackCh)
	}()

	spotifyTracks := []spotify.FullTrack{}
	for track := range trackCh {
		spotifyTracks = append(spotifyTracks, track)
	}

	return spotifyTracks, nil
}
