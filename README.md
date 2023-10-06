# go-clarinet

## Libraries used:
- https://github.com/libp2p/go-libp2p

## Running
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