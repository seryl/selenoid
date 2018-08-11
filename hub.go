package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/aerokube/selenoid/config"
)

// Get preferred outbound ip of this machine
func GetOutboundIP() net.IP {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP
}

type RegistrationInfo struct {
	ListenAddress string
	Timeout       int
	State         *config.State
}

type RegistrationRequest struct {
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	Class         string     `json:"class"`
	Configuration NodeConfig `json:"configuration"`
}

type NodeConfig struct {
	BrowserTimeout             int               `json:"browsertimeout"`
	Capabilities               []HubCapabilities `json:"capabilities"`
	Debug                      bool              `json:"debug"`
	Host                       string            `json:"host"`
	MaxSession                 int               `json:"maxSession"`
	ID                         string            `json:"id"`
	Port                       int               `json:"port"`
	RemoteHost                 string            `json:"remoteHost"`
	Proxy                      string            `json:"proxy"`
	NodeStatusCheckTimeout     int               `json:"nodeStatusCheckTimeout"`
	UnregisterIfStillDownAfter int               `json:"unregisterIfStillDownAfter"`
}

type HubCapabilities struct {
	Browser          string `json:"browserName"`
	Version          string `json:"version"`
	MaxInstances     int    `json:"maxInstances"`
	Platform         string `json:"platform"`
	PlatformName     string `json:"platformName"`
	SeleniumProtocol string `json:"seleniumProtocol"`
}

func generateCapabilities(ri RegistrationInfo) []HubCapabilities {
	capabilities := []HubCapabilities{}

	for browser, versions := range ri.State.Browsers {
		for ver := range versions {
			capabilities = append(capabilities, HubCapabilities{
				Browser:          browser,
				Version:          ver,
				MaxInstances:     ri.State.Total,
				Platform:         runtime.GOOS,
				PlatformName:     runtime.GOOS,
				SeleniumProtocol: "WebDriver",
			})
		}
	}

	return capabilities
}

func createRegistration(ri RegistrationInfo) *RegistrationRequest {
	localAddress := GetOutboundIP().String()
	_, port, _ := net.SplitHostPort(ri.ListenAddress)
	portInt, _ := strconv.Atoi(port)

	return &RegistrationRequest{
		Name:        "selenoid-registration",
		Description: "selenoid node",
		Class:       "org.openqa.grid.common.RegistrationRequest",
		Configuration: NodeConfig{
			BrowserTimeout: ri.Timeout,
			Capabilities:   generateCapabilities(ri),
			Debug:          false,
			MaxSession:     ri.State.Total,
			Host:           localAddress,
			Port:           portInt,
			RemoteHost:     fmt.Sprintf("http://%s", strings.Join([]string{localAddress, port}, ":")),
			ID:             strings.Join([]string{localAddress, port}, ":"),
			Proxy:          "org.openqa.grid.selenium.proxy.DefaultRemoteProxy",
			NodeStatusCheckTimeout:     5000,
			UnregisterIfStillDownAfter: 60000,
		},
	}
}

type HubProxyInfo struct {
	Message string `json:"msg"`
	Success bool   `json:"success"`
}

func hubRegistration(ri RegistrationInfo) {
	ticker := time.NewTicker(5 * time.Second)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	localAddress := GetOutboundIP().String()

	regTimeout := time.Duration(5 * time.Second)
	client := http.Client{
		Timeout: regTimeout,
	}

	b := new(bytes.Buffer)
	json.NewEncoder(b).Encode(createRegistration(ri))
	pi := &HubProxyInfo{}

	for {
		select {
		case <-ticker.C:
			hresp, err := client.Get(fmt.Sprintf("http://%s/grid/api/proxy?id=%s%s", hubAddress, localAddress, listen))
			if err != nil {
				continue
			}

			if hresp.StatusCode == http.StatusOK {
				json.NewDecoder(hresp.Body).Decode(&pi)
				if !pi.Success {
					req, _ := http.NewRequest("POST", fmt.Sprintf("http://%s/grid/register", hubAddress), b)
					req.Header.Set("User-Agent", "selenoid")
					req.Header.Set("Content-Type", "application/json")
					client.Do(req)
				}
			}
		case <-sig:
			ticker.Stop()
			return
		}
	}
}
