# Tendermint Load Testing Framework

`tm-load-test` comes with the ability to define your own load testing
**clients**. A client is the part of the load tester that generates transactions
specific to a particular ABCI application. By default, `tm-load-test` comes with
support for the `kvstore` ABCI app, but what if you want to extend it to test
your own ABCI app?

## Requirements
To follow this guide, you'll need:

* Go v1.12+

## Creating a Custom ABCI Load Testing App

### Step 1: Create your project
You'll effectively have to create your own load testing tool by importing the
`tm-load-test` package into a new project.

```bash
mkdir -p /your/project/
cd /your/project
go mod init github.com/you/my-load-tester
```

### Step 2: Create your load testing client
Create a client that generates transactions for your ABCI app. For an example,
you can look at the [kvstore client code](./client_kvstore.go). Put this 
in `./pkg/myabciapp/client.go`

```go
package myabciapp

import "github.com/informalsystems/tm-load-test/pkg/loadtest"

// MyABCIAppClientFactory creates instances of MyABCIAppClient
type MyABCIAppClientFactory struct {}

// MyABCIAppClientFactory implements loadtest.ClientFactory
var _ loadtest.ClientFactory = (*MyABCIAppClientFactory)(nil)

// MyABCIAppClient is responsible for generating transactions. Only one client
// will be created per connection to the remote Tendermint RPC endpoint, and
// each client will be responsible for maintaining its own state in a
// thread-safe manner.
type MyABCIAppClient struct {}

// MyABCIAppClient implements loadtest.Client
var _ loadtest.Client = (*MyABCIAppClient)(nil)

func (f *MyABCIAppClientFactory) ValidateConfig(cfg loadtest.Config) error {
    // Do any checks here that you need to ensure that the load test 
    // configuration is compatible with your client.
    return nil
}

func (f *MyABCIAppClientFactory) NewClient(cfg loadtest.Config) (loadtest.Client, error) {
    return &MyABCIAppClient{}, nil
}

// GenerateTx must return the raw bytes that make up the transaction for your
// ABCI app. The conversion to base64 will automatically be handled by the 
// loadtest package, so don't worry about that. Only return an error here if you
// want to completely fail the entire load test operation.
func (c *MyABCIAppClient) GenerateTx() ([]byte, error) {
    return []byte("this is my transaction"), nil
}
```

### Step 3: Create your CLI
Create your own CLI in `./cmd/my-load-tester/main.go`:

```go
package main

import (
    "github.com/informalsystems/tm-load-test/pkg/loadtest"
    "github.com/you/my-load-tester/pkg/myabciapp"
)

func main() {
    if err := loadtest.RegisterClientFactory("my-abci-app-name", &myabciapp.MyABCIAppClientFactory{}); err != nil {
        panic(err)
    }
    // The loadtest.Run method will handle CLI argument parsing, errors, 
    // configuration, instantiating the load test and/or coordinator/worker
    // operations, etc. All it needs is to know which client factory to use for
    // its load testing.
    loadtest.Run(&loadtest.CLIConfig{
        AppName:              "my-load-tester",
        AppShortDesc:         "Load testing application for My ABCI App (TM)",
        AppLongDesc:          "Some long description on how to use the tool",
        DefaultClientFactory: "my-abci-app-name",
    })
}
```

For an example of very simple integration testing, you could do something 
similar to what's covered in [integration_test.go](./integration_test.go).

### Step 4: Build your CLI
Then build the executable:

```bash
go build -o ./build/my-load-tester ./cmd/my-load-tester/main.go
```

### Step 5: Run your load test!
Then just follow the [same instructions](../../README.md) as for running the
`tm-load-test` tool to run your own tool. It will use the same command line
parameters.
