package geolookup

import (
	"math"
	"math/rand"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/getlantern/geolookup"
	"github.com/getlantern/golog"

	"github.com/getlantern/flashlight/globals"
	"github.com/getlantern/flashlight/ui"
)

const (
	messageType = `GeoLookup`

	basePublishInterval     = 30 * time.Second
	publishIntervalVariance = basePublishInterval - 10*time.Second
)

var (
	log = golog.LoggerFor("flashlight.geolookup")

	service  *ui.Service
	client   atomic.Value
	cfgMutex sync.Mutex
)

// Configure configures geolookup to use the given http.Client to perform
// lookups. geolookup runs in a continuous loop, periodically updating its
// location and publishing updates to any connected clients. We do this
// continually in order to detect when the computer's location has changed.
func Configure(newClient *http.Client) {
	cfgMutex.Lock()
	defer cfgMutex.Unlock()

	client.Store(newClient)

	if service == nil {
		err := registerService()
		if err != nil {
			log.Errorf("Unable to register service: %v", err)
			return
		}
		go write()
		go read()
		log.Debug("Running")
	}
}

func registerService() error {
	helloFn := func(write func(interface{}) error) error {
		location := globals.GetLocation()
		if location == nil {
			log.Trace("No lastKnownLocation, not sending anything to client")
			return nil
		}
		log.Trace("Sending last known location to new client")
		return write(location)
	}

	var err error
	service, err = ui.Register(messageType, nil, helloFn)
	return err
}

func write() {
	retryWaitTime := 100 * time.Millisecond
	consecutiveFailures := 0

	for {
		// Wait a random amount of time (to avoid looking too suspicious)
		// Note - rand was seeded with the startup time in flashlight.go
		n := rand.Int63n(int64(publishIntervalVariance))
		wait := basePublishInterval - publishIntervalVariance/2 + time.Duration(n)

		oldLocation := globals.GetLocation()
		location, err := geolookup.LookupIPWithClient("", client.Load().(*http.Client))
		if err == nil {
			consecutiveFailures = 0
			if !reflect.DeepEqual(location, oldLocation) {
				log.Debugf("Location changed")
				globals.SetLocation(location)
			}
			// Always publish location, even if unchanged
			service.Out <- location
		} else {
			log.Errorf("Unable to get current location: %v", err)
			// When retrying after a failure, wait a different amount of time
			retryWait := time.Duration(math.Pow(2, float64(consecutiveFailures)) * float64(retryWaitTime))
			if retryWait < wait {
				wait = retryWait
			}
			log.Debugf("Waiting %v before retrying", wait)
			consecutiveFailures += 1
			// If available, publish last known location
			if oldLocation != nil {
				service.Out <- location
			}
		}

		time.Sleep(wait)
	}
}

func read() {
	for _ = range service.In {
		// Discard message, just in case any message is sent to this service.
	}
}
