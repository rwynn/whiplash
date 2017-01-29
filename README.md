whiplash
===
furiously send events from the mongodb oplog to all your clients using SSE 

### Requirements ###
+ [Go](http://golang.org/doc/install)
+ [mgo](http://labix.org/mgo), the mongodb driver for Go
+ [mongodb](http://www.mongodb.org/)
	+ Pass argument --master to mongod to ensure an oplog is created OR
	+ Setup [replica sets](http://docs.mongodb.org/manual/tutorial/deploy-replica-set/) to create oplog
+ [gtm](https://github.com/rwynn/gtm)

### Installation ###

	go get github.com/rwynn/gtm
	go get github.com/rwynn/whiplash

### Getting started - main.go ###
	
	package main

	import "gopkg.in/mgo.v2"
	import "github.com/rwynn/whiplash"
	import "net/http"

	func main() {
		session, err := mgo.Dial("localhost")
		if err != nil {
			panic("Not my tempo!")
		}
		defer session.Close()
		session.SetMode(mgo.Monotonic, true)
		playlist := whiplash.NewPlaylist()
		playlist.Add("/stream", whiplash.AllowAll).Play(session, nil)
		// the second argument to Play allows you to tune gtm.
		// see gtm.Options for what you can pass there
		
		
		// update this next line to point to some directory your user can read
		http.Handle("/",
			http.FileServer(http.Dir("/home/you")))
		

		err = http.ListenAndServe(":9080", nil)
		if err != nil {
			panic("Still not my tempo!")
		}
	}

### Now what? ###

	go run main.go

Here is an example of what you can do

Paste the following into a file called index.html in the directory your specified above

	<html>
	<head>
		<title>Whiplash</title>
		<script src="https://fb.me/react-0.13.3.min.js"></script>
		<script src="https://fb.me/JSXTransformer-0.13.3.js"></script>
	</head>
	<body>
		<div id="whiplash"></div>
		<script>
			// this is our event sourcing store, we only append to it
			var evts = [];
			// this object contains any react components that want to know about new events
			var listeners = window.listeners = [];
			// here we connect to the whiplash event source
			var evtSource = new EventSource("/stream");
			// any time we get a new event we push it and notify all listeners
			evtSource.onmessage = function(e) {
				evts.push(JSON.parse(e.data));
				listeners.forEach(function(l) {
					l.streamChanged(this);
				}, evts);
			}
		</script>
		<script type="text/jsx">
			// this is a reusable mixin that provides some event sourcing utilities
			// it subscribes to new events when the component mounts
			// it also provides some utility functions to process the stream
			var EventSourceMixin = {
				componentDidMount: function() {
			  		this.props.listeners.push(this);
			  	},
			  	componentWillUnmount: function() {
			  		var idx = this.props.listeners.indexOf(this);
			  		this.props.listeners.splice(idx, 1);
			  	},
			  	processStream: function(events, filter) {
			  		return this.collapseStream(filter ? events.filter(filter, this): events);
			  	},
			  	collapseStream: function(events) {
				  	var collapsed = {};
				  	events.forEach(function(e) {
				  		if (e.operation === 'd') {
				  			delete this[e._id];
				  		} else {
				  			this[e._id] = e;
				  		}
				  	}, collapsed);
				  	var props = Object.getOwnPropertyNames(collapsed);
				  	return props.map(function(prop) {
				  		return collapsed[prop];
				  	});
			  	}
			};
			// A React Component for the list of items
			// the list state is a function of all events of the test collection of the test database
			var ItemList = React.createClass({
			  mixins: [EventSourceMixin],
			  componentWillMount: function() {
			  	this.setState({
			  		items: []
			  	});
			  },
			  streamChanged: function(events) {
			  	this.setState({
			  		items: this.processStream(events, function(e) {
			  			return e.namespace === 'test.test';
			  		})
			  	});
			  },
			  render: function() {
			    var items = this.state.items.map(function (item) {
			      return (
			        <div key={item.data._id}>{item.data.text}</div>
			      );
			    });
			    return (
			      <div>
			        {items}
			      </div>
			    );
			  }
			});
			React.render(
	  			<ItemList listeners={window.listeners}></ItemList>,
	  			document.getElementById('whiplash')
			);
		</script>
	</body>
	</html>

Now load [http://localhost:9080](http://localhost:9080)

Now load it in another browser window

### This is not very useful. I just see a blank pages ###

	Fire up mongo and switch to db 'test'
		+ db.test.insert({text: "not my"})
		+ db.test.update({text: "not my"}, {text: "not my tempo"})
		+ db.test.remove({})

### Ok I see stuff streaming to my browser, but what about security? ###

	Replace whiplash.AllowAll in...

	playlist.Add("/stream", whiplash.AllowAll).Play(session)

	With a function that checks the request and/or operation and returns true or false.  
