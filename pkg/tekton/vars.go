package tekton

import (
	"fmt"
	"strings"
)

func replaceVars(input string, opts PipelineOptions) string {
	return replaceVar(input, "COMMIT", shorten(opts.GitCommit))
}

func replaceVar(input, varName, value string) string {
	look := fmt.Sprintf("$%s", varName)

	return strings.ReplaceAll(input, look, value)
}

func shorten(hash string) string {
	if len(hash) > 10 {
		return hash[:10]
	}

	return hash
}
