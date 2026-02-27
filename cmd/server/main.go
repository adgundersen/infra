package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/adgundersen/crimata-infra/internal/api"
	"github.com/adgundersen/crimata-infra/internal/compute"
	"github.com/adgundersen/crimata-infra/internal/dns"
	"github.com/adgundersen/crimata-infra/internal/export"
	"github.com/adgundersen/crimata-infra/internal/instance"
	"github.com/adgundersen/crimata-infra/internal/notify"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	_ "github.com/lib/pq"
)

func main() {
	db, err := sql.Open("postgres", mustEnv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	store := instance.NewStore(db)
	if err := store.Migrate(); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(mustEnv("AWS_REGION")),
	)
	if err != nil {
		log.Fatalf("load aws config: %v", err)
	}

	computeClient := compute.NewClient(awsCfg, compute.Config{
		AMI:             mustEnv("EC2_AMI"),
		InstanceType:    getEnv("EC2_INSTANCE_TYPE", "t3.micro"),
		SecurityGroupID: mustEnv("EC2_SECURITY_GROUP"),
		SubnetID:        mustEnv("EC2_SUBNET"),
	})

	dnsClient := dns.NewClient(awsCfg, dns.Config{
		HostedZoneID: mustEnv("HOSTED_ZONE_ID"),
		BaseDomain:   getEnv("BASE_DOMAIN", "crimata.com"),
	})

	notifyClient := notify.NewClient(awsCfg, notify.Config{
		FromEmail:  getEnv("SES_FROM_EMAIL", "noreply@crimata.com"),
		BaseDomain: getEnv("BASE_DOMAIN", "crimata.com"),
	})

	exportClient := export.NewClient(awsCfg, export.Config{
		S3Bucket: mustEnv("S3_EXPORT_BUCKET"),
		Region:   mustEnv("AWS_REGION"),
	})

	handler := api.NewHandler(store, computeClient, dnsClient, notifyClient, exportClient)

	port := getEnv("PORT", "9000")
	fmt.Printf("crimata-infra listening on :%s\n", port)
	log.Fatal(http.ListenAndServe(":"+port, handler.Routes()))
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("missing required env var: %s", key)
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
