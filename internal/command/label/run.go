package label

import (
	"fmt"
	"github.com/cirruslabs/orchard/pkg/client"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/util/validation"
	"strings"
)

func runLabel(cmd *cobra.Command, args []string) error {
	resourceType := args[0]
	resourceName := args[1]
	add, deleteKey, err := parseLabels(args[2:])
	if err != nil {
		return err
	}
	client, err := client.New()
	if err != nil {
		return err
	}

	switch resourceType {
	case "vm":
		vm, err := client.VMs().Get(cmd.Context(), resourceName)
		if err != nil {
			return err
		}
		if vm.Labels != nil {
			for k, v := range add {
				vm.Labels[k] = v
			}
			for _, v := range deleteKey {
				_, ok := vm.Labels[v]
				if ok {
					delete(vm.Labels, v)
				}
			}
		} else {
			vm.Labels = add
		}
		_, err = client.VMs().Update(cmd.Context(), *vm)
		if err != nil {
			return err
		}
	case "worker":
		worker, err := client.Workers().Get(cmd.Context(), resourceName)
		if err != nil {
			return err
		}
		if worker.Labels != nil {
			for k, v := range add {
				worker.Labels[k] = v
			}
			for _, v := range deleteKey {
				_, ok := worker.Labels[v]
				if ok {
					delete(worker.Labels, v)
				}
			}
		} else {
			worker.Labels = add
		}
		_, err = client.Workers().Update(cmd.Context(), *worker)
		if err != nil {
			return err
		}
	}
	return nil
}

func parseLabels(spec []string) (map[string]string, []string, error) {
	labels := map[string]string{}
	var remove []string
	for _, labelSpec := range spec {
		if strings.Contains(labelSpec, "=") {
			parts := strings.Split(labelSpec, "=")
			if len(parts) != 2 {
				return nil, nil, fmt.Errorf("invalid label spec: %v", labelSpec)
			}
			if errs := validation.IsValidLabelValue(parts[1]); len(errs) != 0 {
				return nil, nil, fmt.Errorf("invalid label value: %q: %s", labelSpec, strings.Join(errs, ";"))
			}
			labels[parts[0]] = parts[1]
		} else if strings.HasSuffix(labelSpec, "-") {
			remove = append(remove, labelSpec[:len(labelSpec)-1])
		} else {
			return nil, nil, fmt.Errorf("unknown label spec: %v", labelSpec)
		}
	}
	for _, removeLabel := range remove {
		if _, found := labels[removeLabel]; found {
			return nil, nil, fmt.Errorf("can not both modify and remove a label in the same command")
		}
	}
	return labels, remove, nil
}
