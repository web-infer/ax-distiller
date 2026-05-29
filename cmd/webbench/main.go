package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
)

func getProjectRoot(cwd string) (dir string, err error) {
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() && e.Name() == ".git" {
			dir = cwd
			return
		}
	}

	if cwd == "/" {
		err = fmt.Errorf("could not find .git project root")
		return
	}

	return getProjectRoot(filepath.Join(cwd, ".."))
}

func getWebDir() (dir string, err error) {
	root, err := getProjectRoot(".")
	if err != nil {
		return
	}
	dir = filepath.Join(root, "cmd", "webbench", "web")
	return
}

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	dir, err := getWebDir()
	if err != nil {
		log.Fatal(err)
	}

	fs := http.FileServer(http.Dir(dir))

	http.Handle("/", fs)

	log.Printf("serving %s at http://localhost%s\n", dir, *addr)
	if err := http.ListenAndServe(*addr, nil); err != nil {
		log.Fatal(err)
	}
}
