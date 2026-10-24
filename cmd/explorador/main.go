// Command explorador serves the EncryptLock explorer (folder explorer/) on
// this computer, so it can be opened in a browser at http://localhost:8080
// or from a phone on the same Wi-Fi. The page itself reads the public data
// from the LockKeeper (port 1317). Later the same folder goes on a free
// static host (Cloudflare Pages / GitHub Pages).
package main

import (
	"flag"
	"log"
	"net/http"
)

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	dir := flag.String("dir", "explorer", "folder with the explorer page")
	flag.Parse()
	log.Printf("Explorador EncryptLock: http://localhost%s", *addr)
	log.Fatal(http.ListenAndServe(*addr, http.FileServer(http.Dir(*dir))))
}
