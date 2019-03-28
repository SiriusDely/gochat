package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/siriusdely/gochat/markov"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

const listenAddr = "localhost:4000"

func main() {
	go netListen()

	http.HandleFunc("/", singleHandler)

	err := http.ListenAndServe(listenAddr, nil)
	if err != nil {
		log.Fatal(err)
	}
}

func netListen() {
	l, err := net.Listen("tcp", "localhost:4001")
	if err != nil {
		log.Fatal(err)
	}

	for {
		c, err := l.Accept()
		if err != nil {
			log.Fatal(err)
		}

		go match(c)
	}
}

type socket struct {
	io.Reader
	io.WriteCloser
	done chan bool
}

func (s socket) Close() error {
	log.Println("socket Close")
	s.WriteCloser.Close()
	s.done <- true
	return nil
}

var upgrader = websocket.Upgrader{}

type conn struct {
	*websocket.Conn
}

func (c *conn) Read(b []byte) (int, error) {
	_, message, err := c.Conn.ReadMessage()
	if err != nil {
		log.Println("Read err:", err)
		return 0, err
	}
	copy(b, []byte(message))
	return len(message), nil
}

func (c *conn) Write(b []byte) (int, error) {
	err := c.Conn.WriteMessage(websocket.TextMessage, b)
	if err != nil {
		log.Println("Write err:", err)
		return 0, err
	}
	return len(b), nil
}

func (c *conn) Close() error {
	log.Println("conn Close")
	return c.Conn.Close()
}

func singleHandler(w http.ResponseWriter, r *http.Request) {
	upgrade := false
	for _, header := range r.Header["Upgrade"] {
		if header == "websocket" {
			upgrade = true
			break
		}
	}

	if upgrade == false {
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		chatTemplate.Execute(w, listenAddr)
		return
	}

	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("Upgrade err:", err)
		return
	}

	r2, w2 := io.Pipe()
	c2 := &conn{c}
	go func() {
		_, err := io.Copy(io.MultiWriter(w2, chain), c2)
		w2.CloseWithError(err)
	}()

	s := socket{r2, c2, make(chan bool)}
	go match(s)
	<-s.done
}

var chain = markov.NewChain(2) // 2-word prefixes
var partner = make(chan io.ReadWriteCloser)

func match(c io.ReadWriteCloser) {
	fmt.Fprintln(c, "Waiting for partner...")

	select {
	case partner <- c:
		// now handled by the other goroutine
	case p := <-partner:
		chat(p, c)
	case <-time.After(5 * time.Second):
		chat(Bot(), c)
	}
}

func chat(a, b io.ReadWriteCloser) {
	fmt.Fprintln(a, "Found one! Say hi.")
	fmt.Fprintln(b, "Found one! Say hi.")

	errc := make(chan error, 1)

	go cp(a, b, errc)
	go cp(b, a, errc)

	if err := <-errc; err != nil {
		log.Println("chat err:", err)
	}
	a.Close()
	b.Close()
}

func cp(w io.Writer, r io.Reader, errc chan<- error) {
	_, err := io.Copy(w, r)
	errc <- err
}

// Bot return an io.ReadWriteCloser that responds to
// each incoming write with a generated sentence.
func Bot() io.ReadWriteCloser {
	r, out := io.Pipe() // for outgoing data
	return bot{r, out}
}

type bot struct {
	io.ReadCloser
	out io.Writer
}

func (b bot) Write(buf []byte) (int, error) {
	go b.speak()
	return len(buf), nil
}

func (b bot) speak() {
	time.Sleep(time.Second)
	msg := chain.Generate(10) // at most 10 words
	b.out.Write([]byte(msg))
}

var chatTemplate, _ = template.ParseFiles("templates/chat.html")
