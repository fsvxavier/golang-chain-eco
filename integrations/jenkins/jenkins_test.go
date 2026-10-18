package jenkins_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fsvxavier/golang-chain-eco/integrations/jenkins"
)

const jobXML = `<?xml version='1.1' encoding='UTF-8'?>
<project><builders><hudson.tasks.Shell><command>echo hello-from-lcg</command></hudson.tasks.Shell></builders></project>`

// JENKINS_URL=http://localhost:8080 [JENKINS_USER/JENKINS_TOKEN]
// (docker run -e JAVA_OPTS=-Djenkins.install.runSetupWizard=false jenkins/jenkins:lts-jdk17)
func TestJenkins(t *testing.T) {
	url := os.Getenv("JENKINS_URL")
	if url == "" {
		t.Skip("JENKINS_URL not set")
	}
	c := &jenkins.Client{URL: url, User: os.Getenv("JENKINS_USER"), Token: os.Getenv("JENKINS_TOKEN")}
	ctx, job := context.Background(), "lcg-job-"+time.Now().Format("150405")
	if err := c.CreateJob(ctx, job, jobXML); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := c.LastBuild(ctx, job); ok || err != nil {
		t.Fatal("fresh job has no build:", ok, err)
	}
	if loc, err := c.Trigger(ctx, job, nil); err != nil || !strings.Contains(loc, "/queue/item/") {
		t.Fatal(loc, err)
	}
	var b jenkins.Build
	for range 60 {
		var ok bool
		var err error
		if b, ok, err = c.LastBuild(ctx, job); err != nil {
			t.Fatal(err)
		} else if ok && !b.Building && b.Result != "" {
			break
		}
		time.Sleep(time.Second)
	}
	if b.Result != "SUCCESS" || b.Number != 1 {
		t.Fatalf("%+v", b)
	}
	out, err := c.Console(ctx, job, b.Number, 0)
	if err != nil || !strings.Contains(out, "hello-from-lcg") {
		t.Fatal(out, err)
	}
	if got := len(c.Tools()); got != 3 {
		t.Fatal(got)
	}
}
