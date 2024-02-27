package label

import (
	"errors"
	"github.com/spf13/cobra"
)

var labelExample = `
		# Update vm 'foo' with the label 'unhealthy' and the value 'true'
		orchard label vm foo unhealthy=true
		# Update worker 'foo' with the label 'unhealthy' and the value 'true'
		orchard label worker foo unhealthy=true`

func NewCommand() *cobra.Command {
	command := &cobra.Command{
		Use:     "label",
		Short:   "Update the labels on a resource",
		Example: labelExample,
		Args:    checkArgs,
		RunE:    runLabel,
	}

	return command
}

func checkArgs(cmd *cobra.Command, args []string) error {
	if len(args) < 3 {
		return errors.New("requires at least three arg")
	}
	if args[0] != "vm" && args[0] != "worker" {
		return errors.New("first argument must be worker or vm")
	}
	return nil
}
