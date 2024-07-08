package rdbs

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	// _ "github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"
)

var rekordboxPath = "/Applications/rekordbox 6/rekordbox.app"

const DBKey = "402fd482c38817c35ffa8ffb8c7d93143b749e7d315df7a81732a1ff43608497"

func getDatabaseFilePath(root string) string {
	return filepath.Join(root, "master.db")
}

func getDataPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		panic("Unable to determine your home directory to location options.json")
	}

	return filepath.Join(homeDir, "/Library/Pioneer/rekordbox")
}

func GetDatabaseDSN(filePath string, encryptionKey string) string {
	dsn := fmt.Sprintf("file:"+filePath+"?_key='%s'", encryptionKey)
	return dsn
}

type RekordboxConfig struct {
	Options  [][]string `json:"options"`
	Defaults struct {
		Mode         string `json:"mode"`
		Connectivity struct {
			URL string `json:"url"`
		} `json:"connectivity"`
		ClockServer struct {
			Urls []string `json:"urls"`
		} `json:"clock_server"`
	} `json:"defaults"`
}

// TODO: put this and other mac specific things into _darwin.go files
func getAsarFilePath(root string) string {
	return filepath.Join(root, "/Contents/MacOS/rekordboxAgent.app/Contents/Resources/app.asar")
}

func getRekordboxConfig() (*RekordboxConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, errors.New("Unable to determine your home directory to location options.json")
	}
	optionsFilePath := filepath.Join(homeDir, "/Library/Application Support/Pioneer/rekordboxAgent/storage/", "options.json")

	// read file
	data, err := os.ReadFile(optionsFilePath)
	if err != nil {
		return nil, err
	}

	// json data
	config := &RekordboxConfig{}

	if err = json.Unmarshal(data, config); err != nil {
		return nil, err
	}

	return config, nil
}

type Config struct{}

func LoadDatabase(_ *Config) (*sql.DB, error) {
	config, err := getRekordboxConfig()
	if err != nil {
		return nil, err
	}

	data := map[string]interface{}{}

	encodedPasswordData := config.Options[1][1]
	fmt.Println(1, encodedPasswordData)
	decodedPasswordData, err := base64.StdEncoding.DecodeString(encodedPasswordData)
	if err != nil {
		return nil, err
	}
	fmt.Println(2, string(decodedPasswordData))
	asarFilePath := getAsarFilePath(rekordboxPath)
	fmt.Println(3, asarFilePath)
	dataPath := getDataPath()
	fmt.Println(6, dataPath)
	databaseFilePath := getDatabaseFilePath(dataPath)
	dsn := GetDatabaseDSN(databaseFilePath, DBKey)
	data["db-path"] = databaseFilePath
	data["db-dsn"] = dsn

	b, err := json.MarshalIndent(data, "", "    ")
	if err != nil {
		return nil, err
	}
	fmt.Println(string(b))

	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	return db, nil
}
