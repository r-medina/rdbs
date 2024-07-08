package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "github.com/xeodou/go-sqlcipher"

	"github.com/r-medina/rdbs"
)

func dumpSchema(db *sql.DB) {
	query := "SELECT type, name, sql FROM sqlite_master WHERE type='table' OR type='index' OR type='view' OR type='trigger';"
	rows, err := db.Query(query)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			typ  string
			name string
			sql  string
		)
		err = rows.Scan(&typ, &name, &sql)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%s: %s\n%s;\n\n", typ, name, sql)
	}

	if err = rows.Err(); err != nil {
		log.Fatal(err)
	}
}

func main() {
	// db, err := rdbs.LoadDatabase(nil)
	// failIfError(err, "could not open sqlite db")
	// defer db.Close()

	log.SetOutput(os.Stderr)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// dsn := rdbs.GetDatabaseDSN(os.Args[1], rdbs.DBKey)
	dsn := fmt.Sprintf("%s?_key='%s'", os.Args[1], rdbs.DBKey)

	fmt.Println("dsn:", dsn)
	db, err := sql.Open("sqlite3", dsn)
	failIfError(err, "opening sqlite database")

	dumpSchema(db)
}

func failIfError(err error, msg string) {
	if err == nil {
		return
	}

	log.Fatalf("%s: %v", msg, err)
}
