package main

import (
	"context"
	"flag"
	"log"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"

	"github.com/ariaservice/terraform-provider-arianet/internal/provider"
)

// Generate the documentation in docs/ from the schema, examples/ and templates/.
//go:generate go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name arianet

// version is set by goreleaser via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	var debug bool
	flag.BoolVar(&debug, "debug", false, "run the provider with support for debuggers like delve")
	flag.Parse()

	err := providerserver.Serve(context.Background(), provider.New(version), providerserver.ServeOpts{
		Address: "registry.terraform.io/ariaservice/arianet",
		Debug:   debug,
	})
	if err != nil {
		log.Fatal(err.Error())
	}
}
