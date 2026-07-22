// Command gitops-demo-contract validates the external Demo GitOps manifest contract.
package main

import (
	"fmt"
	"os"

	"github.com/05allan1213/CloudOps-Copilot/internal/gitopscontract"
)

func main() {
	var err error
	switch {
	case len(os.Args) == 3 && os.Args[1] == "healthy":
		err = gitopscontract.ValidateHealthy(os.Args[2])
	case len(os.Args) == 4 && os.Args[1] == "regression":
		err = gitopscontract.ValidateRegression(os.Args[2], os.Args[3])
	default:
		fmt.Fprintln(os.Stderr, "usage: gitops-demo-contract {healthy DIRECTORY|regression HEALTHY_DIRECTORY REGRESSION_DIRECTORY}")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
	fmt.Println("PASS: fixed five-file Demo GitOps manifest contract")
}
