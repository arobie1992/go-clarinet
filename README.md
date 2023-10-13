# go-clarinet

## Libraries used:
- https://github.com/libp2p/go-libp2p

## Development

### Running
You can run the application like any typical Go application, typically either with `go run main.go` 
or by building it and running the resulting executable. The application requires some configuration
which is specified in a json file. An example of the config format is provided in `example-config.json`.
The path to the config file can be passed as a command line argument when starting the application, 
for example: `go run main.go some/path/config.json`. If no argument is provided, the application will 
attempt to load a file named `config.json` in the current directory.

### Generating a private key
The application requires a private key to give the libp2p node its identity. You can use `scripts/create-cert.sh`
to create a key for you. To run the script execute `create-cert.sh {private key name} {public key name}`. If you 
would like to generate your own, any [PKCS8 key](https://en.wikipedia.org/wiki/PKCS_8) should work.

### Admin endpoints
The application provides some HTTP endpoints under the `/admin` path. These allow triggering of certain actions
to facilitate testing. They are accessible at the port specified in the configuration file. The available
endpoints are:

#### Create connection
Path: `/admin/connect`

Method: `POST`

Body:
```json
{
    "targetNode": "{libp2p ID of the node}"
}
```

Misc: Nodes print their ID upon start-up, for example:
`I am /ip4/127.0.0.1/udp/4433/quic-v1/p2p/QmarutrLy9MdopcWNmYGjvHpEc5PAiz4YtSbgJjrCAZJMm`