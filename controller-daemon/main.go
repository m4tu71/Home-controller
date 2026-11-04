package main

import (
	"fmt"
	"log"
	"os"

	appconfig "webcontroller/config"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	return runWithSocketServer(args, startSocketServer)
}

func runWithSocketServer(args []string, startServer func(string) error) error {
	path, rest, err := appconfig.ParseArgs(args)
	if err != nil {
		return err
	}
	if len(rest) != 0 {
		return fmt.Errorf("unexpected argument %q", rest[0])
	}
	config, err := appconfig.Load(path)
	if err != nil {
		return err
	}
	forwardCommandToPico = func(command string) (string, error) {
		return sendCommandToPicoAt(config.PicoSerialPath, command, configuredPicoSerialOpen)
	}
	return startServer(config.SocketPath)
}
