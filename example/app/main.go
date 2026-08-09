package main

import (
	"log"

	clientgrpc "github.com/omcrgnt/client-grpc"
	"github.com/omcrgnt/app"
	"github.com/omcrgnt/res/unique"
)

const envPrefix = "CLIENT_GRPC_EXAMPLE"

type catalog struct {
	GRPC *clientgrpc.Client `ecfg:"GRPC"`
}

func main() {
	c := catalog{}
	pipeline := app.Pipeline{
		Registry:  unique.Global(),
		EnvPrefix: envPrefix,
	}
	if err := app.Run(&c, pipeline); err != nil {
		log.Fatal(err)
	}
}
