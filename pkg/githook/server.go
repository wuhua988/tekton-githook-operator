package githook

import (
	"fmt"
	"log"
	"net/http"

	"github.com/zhd173/githook/pkg/tekton"
)

// HookServer provides git provider specific functionality
type HookServer interface {
	GetEventHeader() string
	Parse(r *http.Request) (interface{}, error)
	BuildOptionFromPayload(payload interface{}) tekton.PipelineOptions
}

// ReceiveAdapter converts incoming git webhook events to
// CloudEvents and then sends them to the specified Sink
type ReceiveAdapter struct {
	TektonClient *tekton.Client

	HookServer  HookServer
	Namespace   string
	Name        string
	RunSpecJSON string
}

// HandleRequest handles webhook request
func (ra *ReceiveAdapter) HandleRequest(w http.ResponseWriter, r *http.Request) {
	payload, err := ra.HookServer.Parse(r)
	if err != nil {
		log.Println(err)
		http.Error(w, err.Error(), 500)
	}

	ra.HandleEvent(payload, r.Header)
}

// HandleEvent is invoked whenever an event comes in from git
func (ra *ReceiveAdapter) HandleEvent(payload interface{}, header http.Header) {
	err := ra.handleEvent(payload, header)
	if err != nil {
		log.Printf("unexpected error handling git event: %s", err)
	}
}

func (ra *ReceiveAdapter) handleEvent(payload interface{}, header http.Header) error {
	gitEventType := header.Get("X-" + ra.HookServer.GetEventHeader())

	log.Printf("Handling %s", gitEventType)

	if gitEventType == "" {
		return fmt.Errorf("invalid event: %s", gitEventType)
	}

	options := ra.HookServer.BuildOptionFromPayload(payload)
	options.Namespace = ra.Namespace
	options.Prefix = ra.Name
	options.RunSpecJSON = ra.RunSpecJSON

	pipelineRun, err := ra.TektonClient.CreatePipelineRun(options)

	if err != nil {
		return err
	}

	log.Printf("create pipeline run successfully %s", pipelineRun.Name)

	return nil
}
