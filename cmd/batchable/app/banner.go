package app

import (
	"batchable/cmd/batchable/config"
	"os"
	"reflect"

	"github.com/common-nighthawk/go-figure"
	"github.com/jedib0t/go-pretty/v6/table"
)

func printBanner(conf config.Config) {
	myFigure := figure.NewFigure("batchable", "", true)
	myFigure.Print()

	print("\n")

	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetColumnConfigs([]table.ColumnConfig{
		{Number: 0, AutoMerge: true},
	})
	t.AppendHeader(table.Row{"Key", "Value"})

	c := reflect.ValueOf(conf)
	typeOfS := c.Type()

	for i := 0; i < c.NumField(); i++ {
		t.AppendRow(table.Row{typeOfS.Field(i).Name, c.Field(i).Interface()})
	}
	t.SetStyle(table.StyleLight)
	t.Render()
	print("\n")
}
