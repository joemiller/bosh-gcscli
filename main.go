/*
 * Copyright 2017 Google Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *    http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"compress/gzip"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudfoundry/bosh-gcscli/client"
	"github.com/cloudfoundry/bosh-gcscli/config"
	"golang.org/x/net/context"
)

var version = "dev"

// usageExample provides examples of how to use the CLI.
const usageExample = `
# Usage
bosh-gcscli --help

# Upload a blob to the GCS blobstore.
bosh-gcscli -b bucket put <path/to/file> <remote-blob>

# Fetch a blob from the GCS blobstore.
# Destination file will be overwritten if exists.
bosh-gcscli -b bucket get <remote-blob> <path/to/file>

# Remove a blob from the GCS blobstore.
bosh-gcscli -b bucket delete <remote-blob>

# Checks if blob exists in the GCS blobstore.
bosh-gcscli -b bucket exists <remote-blob>

# Generate a signed url for an object
# if an encryption key is present in config, the appropriate header will be sent
# users of the signed url must include encryption headers in request
# Where:
# - <http action> is GET, PUT, or DELETE
# - <expiry> is a duration string less than 7 days (e.g. "6h")
# eg bosh-gcscli -b bucket sign blobid PUT 24h
bosh-gcscli -b bucket sign <remote-blob> <http action> <expiry>`

var (
	showVer      = flag.Bool("v", false, "Print CLI version")
	shortHelp    = flag.Bool("h", false, "Print this help text")
	longHelp     = flag.Bool("help", false, "Print this help text")
	bucket       = flag.String("b", "", "GCS bucket name")
	storageClass = flag.String("storage-class", "", "GCS storage class (defaults to bucket settings")
	compress     = flag.Bool("z", false, "Compress objects with gzip when uploading")

// 	configPath = flag.String("c", "",
// 		`path to a JSON file with the following contents:
// 	{
// 		"bucket_name":         "name of Google Cloud Storage bucket (required)",
// 		"credentials_source":  "Optional, defaults to Application Default Credentials or none)
// 		                        (can be 'static' for a service account specified in json_key),
// 		                        (can be 'none' for explicitly no credentials)"
// 		"json_key":            "JSON Service Account File
// 		                        (optional, required for 'static' credentials)",
// 		"storage_class":       "storage class for objects
// 		                        (optional, defaults to bucket settings)",
// 		"encryption_key":      "Base64 encoded 32 byte Customer-Supplied
// 		                        encryption key used to encrypt objects
// 								(optional, defaults to GCS controlled key)"
// 	}

// 	storage_class is one of MULTI_REGIONAL, REGIONAL, NEARLINE, or COLDLINE.
// 	For more information on characteristics and location compatibility:
// 	    https://cloud.google.com/storage/docs/storage-classes

//	For more information on Customer-Supplied encryption keys:
//		https://cloud.google.com/storage/docs/encryption
//
// `)
)

func main() {
	flag.Parse()

	if *showVer {
		fmt.Printf("version %s\n", version)
		os.Exit(0)
	}

	if *shortHelp || *longHelp || len(flag.Args()) == 0 {
		flag.Usage()
		fmt.Println(usageExample)
		os.Exit(0)
	}

	// if *configPath == "" {
	// 	log.Fatalf("no config file provided\nSee -help for usage\n")
	// }

	// configFile, err := os.Open(*configPath)
	// if err != nil {
	// 	log.Fatalf("opening config %s: %v\n", *configPath, err)
	// }

	// gcsConfig, err := config.NewFromReader(configFile)
	// if err != nil {
	// 	log.Fatalf("reading config %s: %v\n", *configPath, err)
	// }

	if *bucket == "" {
		log.Fatalf("no bucket name provided\nSee -help for usage\n")
	}
	gcsConfig := config.GCSCli{
		BucketName:   *bucket,
		StorageClass: *storageClass,
	}

	ctx := context.Background()
	blobstoreClient, err := client.New(ctx, &gcsConfig)
	if err != nil {
		log.Fatalf("creating gcs client: %v\n", err)
	}

	nonFlagArgs := flag.Args()
	if len(nonFlagArgs) < 2 {
		log.Fatalf("Expected at least two arguments got %d\n", len(nonFlagArgs))
	}

	cmd := nonFlagArgs[0]

	switch cmd {
	case "put":
		if len(nonFlagArgs) != 3 {
			log.Fatalf("put method expected 2 arguments got %d\n", len(nonFlagArgs))
		}
		src, dst := nonFlagArgs[1], nonFlagArgs[2]

		var sourceFile *os.File
		sourceFile, err = os.Open(src)
		if err != nil {
			log.Fatalln(err)
		}

		if *compress {
			pr, pw := io.Pipe()
			gz := gzip.NewWriter(pw)

			go func() {
				defer pw.Close()
				defer gz.Close()
				defer sourceFile.Close()

				_, err := io.Copy(gz, sourceFile)
				if err != nil {
					log.Printf("WARN: gzip failed: %v", err)
				}
			}()

			err = blobstoreClient.Put2(pr, dst, *compress)
			if err != nil {
				log.Fatalf("Upload failed: %v", err)
			}
		} else {
			defer sourceFile.Close()
			err = blobstoreClient.Put2(sourceFile, dst, *compress)
			if err != nil {
				log.Fatalln(err)
				log.Fatalf("Upload failed: %v", err)
			}
		}

	case "get":
		if len(nonFlagArgs) != 3 {
			log.Fatalf("get method expected 2 arguments got %d\n", len(nonFlagArgs))
		}
		src, dst := nonFlagArgs[1], nonFlagArgs[2]

		var dstFile *os.File
		dstFile, err = os.Create(dst)
		if err != nil {
			log.Fatalln(err)
		}

		defer dstFile.Close()
		err = blobstoreClient.Get(src, dstFile)
		if err != nil {
			log.Fatalln(err)
		}
	case "delete":
		if len(nonFlagArgs) != 2 {
			log.Fatalf("delete method expected 2 arguments got %d\n", len(nonFlagArgs))
		}

		err = blobstoreClient.Delete(nonFlagArgs[1])
		if err != nil {
			log.Fatalln(err)
		}
	case "exists":
		if len(nonFlagArgs) != 2 {
			log.Fatalf("exists method expected 2 arguments got %d\n", len(nonFlagArgs))
		}

		var exists bool
		exists, err = blobstoreClient.Exists(nonFlagArgs[1])

		// If the object exists the exit status is 0, otherwise it is 3
		// We are using `3` since `1` and `2` have special meanings
		if err == nil && !exists {
			os.Exit(3)
		}
	case "sign":
		if len(nonFlagArgs) != 4 {
			log.Fatalf("sign method expected 3 arguments got %d\n", len(nonFlagArgs))
		}

		id, action, expiry := nonFlagArgs[1], nonFlagArgs[2], nonFlagArgs[3]

		action = strings.ToUpper(action)
		err = validateAction(action)
		if err != nil {
			log.Fatal(err)
		}

		var expiryDuration time.Duration
		expiryDuration, err = time.ParseDuration(expiry)
		if err != nil {
			log.Fatalf("Invalid expiry duration: %v", err)
		}
		url := ""
		url, err = blobstoreClient.Sign(id, action, expiryDuration)
		if err == nil {
			os.Stdout.WriteString(url)
		}

	default:
		log.Fatalf("unknown command: '%s'\n", cmd)
	}

	if err != nil {
		log.Fatalf("performing operation %s: %s\n", cmd, err)
	}
}

func validateAction(action string) error {
	if action != http.MethodGet && action != http.MethodPut && action != http.MethodDelete {
		return fmt.Errorf("invalid signing action: %s must be GET, PUT, or DELETE", action)
	}
	return nil
}
