package whiplash

import "net/http"
import "gopkg.in/mgo.v2"
import "gopkg.in/mgo.v2/bson"
import "github.com/rwynn/gtm"
import "encoding/json"
import "fmt"
import "bytes"
import "log"

type Message struct {
	Id   string
	Data *gtm.Op
}

// this is how one would implement security
// only by returning true will the op pass to the client
type OpRequestFilter func(*http.Request, *gtm.Op) bool

type Playlist struct {
	// http request path to request filter map
	Filters map[string]OpRequestFilter
}

type Broker struct {
	// Create a map of clients, the keys of the map are the channels
	// over which we can push messages to attached clients. (The values
	// are just booleans and are meaningless.)
	clients map[chan *Message]bool
	// Channel into which new clients can be pushed
	newClients chan chan *Message
	// Channel into which disconnected clients should be pushed
	defunctClients chan chan *Message
	// Channel into which messages are pushed to be broadcast out
	// to attahed clients.
	messages chan *Message
}

type RequestFunc func(http.ResponseWriter, *http.Request)

func AllowAll(request *http.Request, op *gtm.Op) bool {
	return true
}

func ToJsonString(op *gtm.Op) string {
	var buf bytes.Buffer
	result, _ := json.Marshal(op)
	buf.Write(result)
	return buf.String()
}

func Stream(resp http.ResponseWriter, req *http.Request,
	b *Broker, session *mgo.Session, filter OpRequestFilter) {
	defer session.Close()
	f, ok := resp.(http.Flusher)
	if !ok {
		http.Error(resp, "Streaming unsupported!",
			http.StatusInternalServerError)
		return
	}
	c, ok := resp.(http.CloseNotifier)
	if !ok {
		http.Error(resp, "close notification unsupported",
			http.StatusInternalServerError)
		return
	}
	// Create a new channel, over which the broker can
	// send this client messages.
	messageChan := make(chan *Message)
	// Add this client to the map of those that should
	// receive updates
	b.newClients <- messageChan
	defer func() {
		b.defunctClients <- messageChan
	}()
	headers := resp.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	closer := c.CloseNotify()
	for {
		select {
		case msg := <-messageChan:
			if filter(req, msg.Data) {
				fmt.Fprintf(resp, "id: %s\n", msg.Id)
				fmt.Fprintf(resp, "data: %s\n\n", ToJsonString(msg.Data))
				f.Flush()
			}
		case <-closer:
			return
		}
	}
}

func Streamer(b *Broker, session *mgo.Session, filter OpRequestFilter) RequestFunc {
	return func(resp http.ResponseWriter, req *http.Request) {
		Stream(resp, req, b, session.Copy(), filter)
	}
}

func (this *Broker) Start(session *mgo.Session, options *gtm.Options) {
	// tail mongo queuing messages
	go func() {
		ops, errs := gtm.Tail(session, options)
		// Tail returns 2 channels - one for events and one for errors
		for {
			// loop endlessly receiving events
			select {
			case err := <-errs:
				// log errors
				log.Println(err)
			case op := <-ops:
				var idStr string
				switch op.Id.(type) {
				case bson.ObjectId:
					idStr = op.Id.(bson.ObjectId).Hex()
				default:
					idStr = fmt.Sprintf("%v", op.Id)
				}
				message := &Message{idStr, op}
				this.messages <- message
			}
		}
	}()

	// Start a goroutine to broker messages
	go func() {
		// Loop endlessly
		for {
			// Block until we receive from one of the
			// three following channels.
			select {
			case s := <-this.newClients:
				// There is a new client attached and we
				// want to start sending them messages.
				this.clients[s] = true
			case s := <-this.defunctClients:
				// A client has dettached and we want to
				// stop sending them messages.
				delete(this.clients, s)
			case msg := <-this.messages:
				// There is a new message to send. For each
				// attached client, push the new message
				// into the client's message channel.
				for s, _ := range this.clients {
					s <- msg
				}
			}
		}
	}()
}

func NewBroker() *Broker {
	b := &Broker{
		make(map[chan *Message]bool),
		make(chan (chan *Message)),
		make(chan (chan *Message)),
		make(chan *Message),
	}
	return b
}

func NewPlaylist() *Playlist {
	p := &Playlist{
		make(map[string]OpRequestFilter),
	}
	return p
}

func (this *Playlist) Add(path string, filter OpRequestFilter) *Playlist {
	this.Filters[path] = filter
	return this
}

func (this *Playlist) Play(session *mgo.Session, options *gtm.Options) {
	broker := NewBroker()
	broker.Start(session, options)
	for k, v := range this.Filters {
		http.HandleFunc(k, Streamer(broker, session, v))
	}
}
