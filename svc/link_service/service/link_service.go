package service

import (
	"errors"
	"fmt"
	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/gorilla/mux"
	"github.com/the-gigi/delinkcious/pkg/db_util"
	lm "github.com/the-gigi/delinkcious/pkg/link_manager"
	"github.com/the-gigi/delinkcious/pkg/link_manager_events"

	"github.com/the-gigi/delinkcious/pkg/log"
	"github.com/the-gigi/delinkcious/pkg/metrics"

	om "github.com/the-gigi/delinkcious/pkg/object_model"
	sgm "github.com/the-gigi/delinkcious/pkg/social_graph_client"
	"net/http"
	"os"
	"strconv"
)

type EventSink struct {
}

type linkManagerMiddleware func(om.LinkManager) om.LinkManager

func (s *EventSink) OnLinkAdded(username string, link *om.Link) {
	//log.Println("Link added")
}

func (s *EventSink) OnLinkUpdated(username string, link *om.Link) {
	//log.Println("Link updated")
}

func (s *EventSink) OnLinkDeleted(username string, url string) {
	//log.Println("Link deleted")
}

func Run() {
	dbHost, dbPort, err := db_util.GetDbEndpoint("link")
	if err != nil {
		log.Fatal(err)
	}
	store, err := lm.NewDbLinkStore(dbHost, dbPort, "postgres", "postgres")
	if err != nil {
		log.Fatal(err)
	}

	sgHost := os.Getenv("SOCIAL_GRAPH_MANAGER_SERVICE_HOST")
	if sgHost == "" {
		sgHost = "localhost"
	}

	sgPort := os.Getenv("SOCIAL_GRAPH_MANAGER_SERVICE_PORT")
	if sgPort == "" {
		sgPort = "9090"
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	maxLinksPerUserStr := os.Getenv("MAX_LINKS_PER_USER")
	if maxLinksPerUserStr == "" {
		maxLinksPerUserStr = "10"
	}

	maxLinksPerUser, err := strconv.ParseInt(os.Getenv("MAX_LINKS_PER_USER"), 10, 64)
	if err != nil {
		log.Fatal(err)
	}

	socialGraphClient, err := sgm.NewClient(fmt.Sprintf("%s:%s", sgHost, sgPort))
	if err != nil {
		log.Fatal(err)
	}

	natsHostname := os.Getenv("NATS_CLUSTER_SERVICE_HOST")
	natsPort := os.Getenv("NATS_CLUSTER_SERVICE_PORT")

	natsUrl := ""
	var eventSink om.LinkManagerEvents
	if natsHostname != "" {
		natsUrl = natsHostname + ":" + natsPort
		eventSink, err = link_manager_events.NewEventSender(natsUrl)
		if err != nil {
			log.Fatal(err)
		}
	} else {
		eventSink = &EventSink{}
	}

	// Create a logger
	logger := log.NewLogger("link manager")

	// Create the service implementation
	svc, err := lm.NewLinkManager(store, socialGraphClient, natsUrl, eventSink, maxLinksPerUser)
	if err != nil {
		log.Fatal(err)
	}

	// Hook up the logging middleware to the service and the logger
	svc = newLoggingMiddleware(logger)(svc)

	requestCounter := metrics.NewCounter("link", "request_count", "total count requests for a service method")
	if requestCounter == nil {
		log.Fatal(errors.New("Failed to create request counter"))
	}

	requestSummary := metrics.NewSummary("link", "request_count", "total duration of requests for a service method")

	// Hook up the metrics middleware
	svc = newMetricsMiddleware(requestCounter, requestSummary)(svc)



	getLinksHandler := httptransport.NewServer(
		makeGetLinksEndpoint(svc),
		decodeGetLinksRequest,
		encodeResponse,
	)

	addLinkHandler := httptransport.NewServer(
		makeAddLinkEndpoint(svc),
		decodeAddLinkRequest,
		encodeResponse,
	)

	updateLinkHandler := httptransport.NewServer(
		makeUpdateLinkEndpoint(svc),
		decodeUpdateLinkRequest,
		encodeResponse,
	)

	deleteLinkHandler := httptransport.NewServer(
		makeDeleteLinkEndpoint(svc),
		decodeDeleteLinkRequest,
		encodeResponse,
	)

	r := mux.NewRouter()
	r.Methods("GET").Path("/links").Handler(getLinksHandler)
	r.Methods("POST").Path("/links").Handler(addLinkHandler)
	r.Methods("PUT").Path("/links").Handler(updateLinkHandler)
	r.Methods("DELETE").Path("/links").Handler(deleteLinkHandler)

	logger.Log("msg", "*** listening on ***", "port", port)
	log.Fatal(http.ListenAndServe(":"+port, r))
}
