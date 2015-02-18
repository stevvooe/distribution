package main

import (
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/codegangsta/cli"
)

func main() {
	logrus.SetOutput(os.Stderr)
	app := cli.NewApp()
	app.Name = "dist"
	app.Usage = "Package and ship Docker content"

	app.Action = commandList.Action
	app.Commands = []cli.Command{
		commandList,
		commandPull,
		commandPush,
		commandMount,
		commandSnapshot,
		commandPack,
	}

	app.RunAndExitOnError()
}
