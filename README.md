# go-clarinet

A Go implementation of the Clarinet protocol. The Clarinet protocol is designed to facilitate exchanging data between
nodes in a peer-to-peer network. Each connection is composed of three nodes: the sender, the receiver, and a witness.
All three nodes record the data that was exchanged, and the witness is present to create a minimal consensus protocol
in the event of disagreements between sender and receiver about what data was transmitted.

More importantly, Clarinet also provides facilities to help determine the trustworthiness of nodes in the network. This
is key for witness selection so that the sender can have some level of confidence that the witness is not in collusion
with a malicious receiver. At the moment, only the sender has the ability to select the witness for a given connection.
There are plans to allow more flexibility in witness selection down the line. Three fundamental actions are present for
reputation assessment: Reward, StrongPenalize, and WeakPenalize. Rewards are applied when everything is as expected.
WeakPenalize is applied when a node notices discrepancies but is unsure of the origin of the discrepancies. It applies
WeakPenalize to all other nodes in the connection. StrongPenalize is applied when a node detects discrepancies and is
sure of the origin. It applies StrongPenalize only to the node that is the origin of the discrepancy. Discrepancies are 
currently detected through cryptographic signatures and data comparisons.

## Libraries used:
- https://github.com/libp2p/go-libp2p
- https://gorm.io/
- https://www.sqlite.org/index.html
- https://github.com/uber-go/zap

#### Generating a private key
The application requires a private key to give the libp2p node its identity. You can use `scripts/create-cert.sh`
to create a key for you. To run the script execute `create-cert.sh {private key name} {public key name}`. If you 
would like to generate your own, any [PKCS8 key](https://en.wikipedia.org/wiki/PKCS_8) should work.

#### Setting up and connecting DB
You can let the app create the DB by just specifying the file in the configuration or you can create the DB
beforehand by running the command `sqlite3 {db name}.db` and using the path to this file in your configuration.
If you use the sqlite command, this will also open up the [cli](https://sqlite.org/cli.html).

### Using the library
This library can be used the same as any other Go library. To start the node, you must call the 
[start](#start-node) method. See the [Operations](#operations) section for details on the available
and supported operations.

#### Misc
When a node starts, it will print the following line so you can use its fully qualified address:
`I am /ip4/127.0.0.1/udp/4433/quic-v1/p2p/QmarutrLy9MdopcWNmYGjvHpEc5PAiz4YtSbgJjrCAZJMm`

### Operations
This section details the exported operations that users of the library can leverage. While other functions
may be exported, only these should be used. Code will likely be refactored for cleanup and will likely
include removing public access to all other functions.

#### Start Node
`goclarinet.Start(configPath string)`

Starts the Clarinet Node. The `configPath` parameter can point to a file containing your configuration. If 
an empty string is provided, the library will search for a file named `config.json` in the current directory.
See the [Configuration](#configuration) section for details on the available options.

#### Add Peer
`p2p.AddPeer(peerAddress string)`

This should be the fully qualified multiaddress of the peer you wish to add. This can be found in the printout
detailed in the [Misc](#misc) section. It must include the `/p2p/{node ID}` section to successfully add the peer.

#### Create connection
`control.RequestConnection(targetNode string)`

Contact the `targetNode` and request a connection. At the moment, only the node wishing to send may create a
connection. This also transparently includes witness selection.

#### Send Data
`p2p.SendData(connID uuid.UUID, data []byte)`

Send data over the specified connection. The connection must be open.

#### Close Connection
`control.CloseConnection(connID uuid.UUID)`

Close the specified connection. If the connection is already closed, operation will be a no-op.

#### Query Peer for Message
`control.QueryForMessage(nodeAddr string, conn p2p.Connection, seqNo int)`

Query the specified peer for the specified message on the specified connection and apply appropriate reputation
actions. A node may query the same peer for the same message any number of times. Currently, the users of 
the library must track which nodes and messages they have queried for.

#### Request Peers from Peer
`control.SendRequestPeersRequest(targetNode string, numPeers int)`

Request the addresses of a number of peers from the target node. This list of peers excludes the requesting 
node and the target node. The `numPeers` parameter is the maximum number of addresses the requesting node 
wishes to receive. The target node may return fewer if it does not know that many peers, but should not return
more. If `numPeers` is negative, it indicates that the requesting node wishes to receive all the addresses
known by the target node.

### Admin Endpoints
The app provides a set of REST endpoints to allow for triggering actions externally. These map pretty directly
to the operations detailed in the [Operations](#operations) section. Details on how to use them will be added
later.

### Configuration
The following configuration options are available. If a default exists, it is provided with the field. The
config file is a JSON file with the parameters. You can reference an example in the `example-config.json`
included in the project.

- `libp2p.port`: The port the node will be accessible on.
- `libp2p.certPath`: The path to the RSA private key the node will use.
- `libp2p.dbPath`: The path to the sqlite3 database file the app will use.
- `admin.port`: The port the admin endpoints will be accessible on.