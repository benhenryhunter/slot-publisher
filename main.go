package main

import (
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strings"

	"github.com/r3labs/sse"

	browser "github.com/EDDYCJY/fake-useragent"
)

var defaultRelays = []string{
	"https://boost-relay.flashbots.net",
	"https://bloxroute.regulated.blxrbdn.com",
	"https://bloxroute.max-profit.blxrbdn.com",
	"https://relay.ultrasound.money",
	"https://agnostic-relay.net",
	"https://aestus.live",
}

var defaultRelaysOfInterest = map[string]struct{}{
	"https://bloxroute.regulated.blxrbdn.com":  {},
	"https://bloxroute.max-profit.blxrbdn.com": {},
}

var (
	beaconClientHost     string // beacon client host
	relayURLFlag         string // comma separated list of relay urls
	relaysOfInterestFlag string // comma separated list of relay urls

	relayURLs        []string            // urls of all relays to check if they received the submission
	relaysOfInterest map[string]struct{} // relays of interest (e.g. bloxroute)
)

func init() {
	flag.StringVar(&beaconClientHost, "beacon-client", "http://localhost:5052", "beacon client host")
	flag.StringVar(&relayURLFlag, "relays", "", "comma separated list of relay urls")
	flag.StringVar(&relaysOfInterestFlag, "relays-of-interest", "", "comma separated list of relay urls")
}

func main() {

	flag.Parse()

	if relayURLFlag != "" {
		relayURLs = strings.Split(relayURLFlag, ",")
	} else {
		relayURLs = defaultRelays
	}

	if relaysOfInterestFlag != "" {
		for _, relay := range strings.Split(relaysOfInterestFlag, ",") {
			relaysOfInterest[relay] = struct{}{}
		}

	} else {
		relaysOfInterest = defaultRelaysOfInterest
	}

	url := fmt.Sprintf("%v/eth/v1/events?topics=payload_attributes", beaconClientHost)

	fmt.Println("Starting to listen for beacon events at ", url)

	client := sse.NewClient(url)

	err := client.SubscribeRaw(func(msg *sse.Event) {
		data := payloadAttributeEvent{}
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			log.Print("could not process beacon event, msg data", "msg", msg.Data, "err", err)
			return
		}

		relays, err := checkPublishing(data.Data.ProposalSlot-2, relayURLs)
		if err != nil {
			log.Print("could not check publishing", "err", err)
			return
		}

		fmt.Println("Relays that published the block for slot", "slot", data.Data.ProposalSlot-2, "relays", relays)

	})

	if err != nil {
		log.Fatal(err)
	}
}

func checkPublishing(slot uint64, relays []string) (map[string]bool, error) {
	client := http.Client{}
	relaySubmissions := make(map[string]bool)
	for _, relay := range relays {
		req, err := http.NewRequest("GET", fmt.Sprintf("%v/relay/v1/data/bidtraces/proposer_payload_delivered?slot=%v", relay, slot), nil)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("could not get bidtrace data from relay", "relay", relay, "err", err)
			continue
		}

		if resp.StatusCode != 200 {
			fmt.Println("could not get bidtrace data from relay", "relay", relay, "status", resp.StatusCode)
			continue
		}

		var data []blockSubmission
		if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
			fmt.Println("could not decode bidtrace data from relay", "relay", relay, "err", err)
			continue
		}

		if len(data) > 0 {
			relaySubmissions[relay] = true
		}
	}
	return relaySubmissions, nil
}

func validatorIndexToPubkey(beaconClient, index string) (string, error) {

	url := fmt.Sprintf("%v/eth/v1/beacon/states/head/validators?id=%s", beaconClient, index)

	client := http.Client{}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("could not get validator data", "err", err)
		return "", err
	}

	if resp.StatusCode != 200 {
		fmt.Println("could not get validator data", "status", resp.StatusCode)
		return "", err
	}

	var data validatorResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		fmt.Println("could not decode validator data", "err", err)
		return "", err
	}

	if len(data.Data) > 0 {
		return data.Data[0].Validator.Pubkey, nil
	}

	return "", nil
}

func getRegistration(relays []string, pubkey string) []string {
	registeredRelays := []string{}
	for _, relayURL := range relays {
		buf := make([]byte, 4)
		ip := rand.Uint32()

		binary.LittleEndian.PutUint32(buf, ip)
		// randomIP := net.IP(buf).String()
		client := http.Client{}

		url := fmt.Sprintf("%s/relay/v1/data/validator_registration?pubkey=%s", relayURL, pubkey)
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			fmt.Printf("could not get registration from %s\n", url)
			continue
		}

		req.Header.Set("User-Agent", browser.Random())

		res, err := client.Do(req)
		if err != nil {
			fmt.Printf("could not get registration from %s\n", url)
			continue
		}

		if res.StatusCode == http.StatusOK {
			registeredRelays = append(registeredRelays, relayURL)
		}
	}
	return registeredRelays
}
