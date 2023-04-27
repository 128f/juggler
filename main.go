package main

import (
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/digitalocean/go-libvirt"
	"github.com/urfave/cli/v2"

	"github.com/tdewolff/minify/v2"
	"github.com/tdewolff/minify/v2/xml"
)

// Reads the devices from a directory
func readDeviceFiles(dir string) map[string]string {
	log.Println("Reading files from ", dir)
	documents := map[string]string{}
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		if err != nil {
			return err
		}
		file, _ := os.Open(path)
		datafile, _ := ioutil.ReadAll(file)
		documents[info.Name()] = string(datafile)
		defer file.Close()
		return nil
	})
	return documents
}

type DeviceConnector struct {
	client *libvirt.Libvirt
}

func NewDeviceConnector() DeviceConnector {
	deviceConnector := DeviceConnector{client: nil}
	// TODO: config
	socket := "/var/snap/microstack/common/run/libvirt/libvirt-sock"
	c, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		log.Fatalf("failed to dial libvirt: %v", err)
	}

	l := libvirt.New(c)
	if err := l.Connect(); err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	deviceConnector.client = l

	v, err := l.Version()
	if err != nil {
		log.Fatalf("failed to retrieve libvirt version: %v", err)
	}
	log.Println("Connected to libvirt! Version:", v)

	return deviceConnector
}

func (deviceConnector DeviceConnector) Disconnect() {
	if err := deviceConnector.client.Disconnect(); err != nil {
		log.Fatalf("failed to disconnect: %v", err)
	}
}

// Attach an xml-defined device to the given domain
func (deviceConnector DeviceConnector) attachDevices(domain libvirt.Domain, deviceXML map[string]string) {
	log.Printf("Attaching %d deviceXML to %s", len(deviceXML), domain.Name)
	var filenames []string
	for f := range deviceXML {
		filenames = append(filenames, f)
	}
	sort.Sort(sort.StringSlice(filenames))
	for deviceName, device := range deviceXML {
		err := deviceConnector.client.DomainAttachDevice(domain, device)
		if err != nil {
			log.Fatalf("Failed to attach device at index %s: %v", deviceName, err)
		}
	}
}

// Detach the devices using the given mapping,
// both maps keyed by filename and map to XML and domains respectively
func (deviceConnector DeviceConnector) detachDevices(deviceDomains map[string]libvirt.Domain, deviceXML map[string]string) {
	var filenames []string
	for f := range deviceDomains {
		filenames = append(filenames, f)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(filenames)))
	for _,filename := range filenames {
		domain := deviceDomains[filename]
		xml := deviceXML[filename]
		err := deviceConnector.client.DomainDetachDevice(domain, xml)
		if err != nil {
			log.Fatalf("Failed to detach device %s to domain %s: %v", filename, domain.Name, err)
		}
	}
}

// Make sure the given devices are not attached to any domain
func (deviceConnector DeviceConnector) orphanDevices(devices map[string]string) {
	domains, err := deviceConnector.client.Domains()
	if err != nil {
		log.Fatalf("failed to retrieve domains: %v", err)
	}
	log.Printf("Detaching %d devices from all domains", len(devices))
	// Naively just grinding through every domain and attempting to eject
	for _, domain := range domains {
		for deviceName, device := range devices {

			log.Printf("Detaching %s from all domains", deviceName)
			// It doesn't really matter if there is an error here,
			// because every machine that _doesn't_ have a device attached is expected to error
			// TODO: do this in a more careful and efficient manner
			err := deviceConnector.client.DomainAttachDevice(domain, device)
			if err != nil {
				log.Printf("Failed to detach device %s from domain %s: %v", deviceName, domain.Name, err)
			}
		}
	}
}

func (deviceConnector DeviceConnector) findDevices(devices map[string]string) map[string]libvirt.Domain {
	// TODO: responsibly handle errors
	output := make(map[string]libvirt.Domain)
	domains, err := deviceConnector.client.Domains()
	if err != nil {
		log.Fatalf("failed to retrieve domains: %v", err)
	}
	log.Printf("Searching for %d devices in all domains", len(devices))
	m := minify.New()
	m.AddFuncRegexp(regexp.MustCompile("[/+]xml$"), xml.Minify)
	// Naively just grinding through every domain and attempting to eject
	for _, domain := range domains {
		xml,err := deviceConnector.client.DomainGetXMLDesc(domain, 0)
		if err != nil {
			log.Printf("Failed to read xml for domain %s: %v", domain.Name, err)
			continue
		}
		xml,_ = m.String("text/xml", xml)
		xml = strings.ToLower(xml)
		for deviceName, device := range devices {
			log.Printf("Searching for %s from domain %s", deviceName, domain.Name)
			deviceMinified,_ := m.String("text/xml", device)
			deviceMinified = strings.ToLower(deviceMinified)
			if strings.Contains(xml, deviceMinified) {
				output[deviceName] = domain
			}
		}
	}
	return output
}

func (deviceConnector DeviceConnector) getDomains() []libvirt.Domain {
	domains, err := deviceConnector.client.Domains()
	if err != nil {
		log.Fatalf("failed to retrieve domains: %v", err)
	}
	return domains
}

func (deviceConnector DeviceConnector) listDomains() {
	domains, err := deviceConnector.client.Domains()
	if err != nil {
		log.Fatalf("failed to retrieve domains: %v", err)
	}
	log.Println("ID\tName\t\tUUID")
	log.Printf("--------------------------------------------------------\n")
	for _, d := range domains {
		log.Printf("%d\t%s\t%x\n", d.ID, d.Name, d.UUID)
	}
}

func main() {
	app := &cli.App{
		Name:  "juggler",
		Usage: "Juggle your PCI devices among libvirt VMs! \njuggler will attach, detach a directory PCI devices to/from a domain in alphabetical order",
		Commands: []*cli.Command{
			{
				Name:  "attach",
				Usage: "Attach a set of devices to a domain",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "dir",
						Usage: "The directory we will scan for device xml files",
					},
					&cli.StringFlag{
						Name:  "domain-name",
						Usage: "The name of the domain to attach the devices to",
					},
				},
				Action: func(cCtx *cli.Context) error {
					devices := readDeviceFiles(cCtx.String("dir"))
					connector := NewDeviceConnector()
					domainName := cCtx.String("domain-name")
					domain,err := connector.client.DomainLookupByName(domainName)
					if err != nil {
						log.Fatalf("Failed to lookup domain by name %s: %v", domainName, err)
					}
					connector.attachDevices(domain, devices)
					return nil
				},
			},
			{
				Name:  "detach",
				Usage: "Detach devices from all domains",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "dir",
						Usage: "The directory we will scan for device xml files",
					},
				},
				Action: func(cCtx *cli.Context) error {
					devices := readDeviceFiles(cCtx.String("dir"))
					connector := NewDeviceConnector()
					attachments := connector.findDevices(devices)
					connector.detachDevices(attachments, devices)
					return nil
				},
			},
			{
				Name:  "find",
				Usage: "Find which domain has the given devices",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:  "dir",
						Usage: "The directory we will scan for device xml files",
					},
				},
				Action: func(cCtx *cli.Context) error {
					devices := readDeviceFiles(cCtx.String("dir"))
					connector := NewDeviceConnector()
					listing := connector.findDevices(devices)
					for deviceFileName,domain := range listing {
						log.Printf("%s : %s\n", deviceFileName, domain.Name)
					}
					return nil
				},
			},
		},
	}
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
