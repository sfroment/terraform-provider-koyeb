package koyeb

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/koyeb/koyeb-api-client-go/api/v1/koyeb"
)

// NOTE: The SDKv2 acceptance harness runs an unconditional post-test
// terraform destroy and fails the test on any destroy error
// (helper/resource testing_new.go). DELETE /v1/service_pools/{id} is
// implemented here but not yet live in prod, so against prod the destroy
// step fails and leaks the created pool. These tests are therefore
// skipped unless explicitly opted in with KOYEB_SERVICE_POOL_ACC=1.
// No sweeper or CheckDestroy is registered for the same reason. Remove
// the gate when the delete endpoint ships in prod.

func TestAccKoyebServicePool_Basic(t *testing.T) {
	if os.Getenv("KOYEB_SERVICE_POOL_ACC") != "1" {
		t.Skip("Acceptance tests for koyeb_service_pool are opt-in (KOYEB_SERVICE_POOL_ACC=1): " +
			"the delete endpoint is not live in prod yet, so the harness's post-test destroy " +
			"would fail and leak the created pool.")
	}

	var pool koyeb.ServicePool
	poolName := randomTestName()

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(testAccKoyebServicePoolConfig_basic, poolName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckKoyebServicePoolExists("koyeb_service_pool.foo", &pool),
					resource.TestCheckResourceAttr("koyeb_service_pool.foo", "name", poolName),
					resource.TestCheckResourceAttr("koyeb_service_pool.foo", "size", "1"),
					resource.TestCheckResourceAttrSet("koyeb_service_pool.foo", "id"),
					resource.TestCheckResourceAttrSet("koyeb_service_pool.foo", "organization_id"),
					resource.TestCheckResourceAttrSet("koyeb_service_pool.foo", "ready_count"),
					resource.TestCheckResourceAttrSet("koyeb_service_pool.foo", "status"),
					resource.TestCheckResourceAttrSet("koyeb_service_pool.foo", "updated_at"),
					resource.TestCheckResourceAttrSet("koyeb_service_pool.foo", "created_at"),
				),
			},
			{
				Config: fmt.Sprintf(testAccKoyebServicePoolConfig_updated, poolName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckKoyebServicePoolExists("koyeb_service_pool.foo", &pool),
					resource.TestCheckResourceAttr("koyeb_service_pool.foo", "size", "2"),
				),
			},
		},
	})
}

func testAccCheckKoyebServicePoolExists(n string, pool *koyeb.ServicePool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No Record ID is set")
		}

		client := testAccProvider.Meta().(*koyeb.APIClient)

		res, _, err := client.ServicePoolsApi.GetServicePool(context.Background(), rs.Primary.ID).Execute()
		if err != nil {
			return err
		}

		servicePool := res.GetServicePool()
		if servicePool.GetId() != rs.Primary.ID {
			return fmt.Errorf("Record not found")
		}

		*pool = servicePool

		return nil
	}
}

const testAccKoyebServicePoolConfig_basic = `
resource "koyeb_service_pool" "foo" {
	name = "%s"
	size = 1
	definition {
		name = "pool"
		instance_types {
			type = "micro"
		}
		ports {
			port     = 3000
			protocol = "http"
		}
		scalings {
			min = 1
			max = 1
		}
		regions = ["tyo"]
		docker {
			image = "koyeb/demo"
		}
	}
}`

const testAccKoyebServicePoolConfig_updated = `
resource "koyeb_service_pool" "foo" {
	name = "%s"
	size = 2
	definition {
		name = "pool"
		instance_types {
			type = "micro"
		}
		ports {
			port     = 3000
			protocol = "http"
		}
		scalings {
			min = 1
			max = 1
		}
		regions = ["tyo"]
		docker {
			image = "koyeb/demo"
		}
	}
}`

func TestResourceKoyebServicePoolRenameReturnsNotSupportedError(t *testing.T) {
	d := schema.TestResourceDataRaw(t, servicePoolSchema(), map[string]interface{}{
		"name": "renamed-pool",
		"size": 1,
	})
	d.SetId("pool-id")

	diags := resourceKoyebServicePoolUpdate(context.Background(), d, nil)

	if len(diags) != 1 {
		t.Fatalf("expected exactly 1 diagnostic, got %d", len(diags))
	}
	if diags[0].Severity != diag.Error {
		t.Errorf("expected an error diagnostic, got severity %v", diags[0].Severity)
	}
	if !strings.Contains(diags[0].Summary, "Renaming service pools is not supported") {
		t.Errorf("expected an explicit rename-not-supported error, got: %s", diags[0].Summary)
	}
	if got := d.Id(); got != "pool-id" {
		t.Errorf("expected the pool ID to be preserved, got %q", got)
	}
}

func TestResourceKoyebServicePoolDeleteCallsAPIAndClearsID(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("{}"))
	}))
	defer srv.Close()

	cfg := koyeb.NewConfiguration()
	cfg.Servers[0].URL = srv.URL

	d := schema.TestResourceDataRaw(t, servicePoolSchema(), map[string]interface{}{
		"name": "my-pool",
		"size": 1,
	})
	d.SetId("pool-id")

	diags := resourceKoyebServicePoolDelete(context.Background(), d, koyeb.NewAPIClient(cfg))

	if len(diags) != 0 {
		t.Fatalf("expected no diagnostics, got %v", diags)
	}
	if gotMethod != http.MethodDelete {
		t.Errorf("expected a DELETE request, got %s", gotMethod)
	}
	if gotPath != "/v1/service_pools/pool-id" {
		t.Errorf("expected the request path /v1/service_pools/pool-id, got %s", gotPath)
	}
	if got := d.Id(); got != "" {
		t.Errorf("expected the pool ID to be cleared, got %q", got)
	}
}

func TestSetServicePoolAttributeMapsServicePoolToState(t *testing.T) {
	d := schema.TestResourceDataRaw(t, servicePoolSchema(), map[string]interface{}{
		"name": "stale-seed-pool",
		"size": 1,
	})

	pool := koyeb.ServicePool{
		Id:             toOpt("pool-123"),
		Name:           toOpt("my-pool"),
		Size:           toOpt(int64(2)),
		ReadyCount:     toOpt(int64(1)),
		OrganizationId: toOpt("org-123"),
		WorkspaceId:    toOpt("ws-123"),
		CustomerId:     toOpt("cust-123"),
		Generation:     toOpt("7"),
		Status:         toOpt(koyeb.SERVICEPOOLSTATUS_READY),
		Messages:       []string{"provisioned", "scaled"},
		CreatedAt:      toOpt(time.Date(2026, time.September, 17, 7, 51, 3, 0, time.UTC)),
		UpdatedAt:      toOpt(time.Date(2026, time.September, 17, 8, 30, 0, 0, time.UTC)),
		Definition: &koyeb.DeploymentDefinition{
			Name: toOpt("pool-def"),
			Type: toOpt(koyeb.DeploymentDefinitionType("WEB")),
			Docker: &koyeb.DockerSource{
				Image: toOpt("koyeb/demo"),
			},
			InstanceTypes: []koyeb.DeploymentInstanceType{
				{Type: toOpt("micro")},
			},
			Regions: []string{"fra"},
		},
	}

	if err := setServicePoolAttribute(d, pool); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if got := d.Id(); got != "pool-123" {
		t.Errorf("unexpected ID: %q", got)
	}
	if got := d.Get("name"); got != "my-pool" {
		t.Errorf("unexpected name: %v", got)
	}
	if got := d.Get("size"); got != 2 {
		t.Errorf("unexpected size: %v", got)
	}
	if got := d.Get("ready_count"); got != 1 {
		t.Errorf("unexpected ready_count: %v", got)
	}
	if got := d.Get("status").(string); got != "READY" {
		t.Errorf("unexpected status: %q", got)
	}
	if got := d.Get("messages"); got != "provisioned scaled" {
		t.Errorf("unexpected messages: %v", got)
	}
	if got := d.Get("generation"); got != "7" {
		t.Errorf("unexpected generation: %v", got)
	}
	if got := d.Get("organization_id"); got != "org-123" {
		t.Errorf("unexpected organization_id: %v", got)
	}
	if got := d.Get("workspace_id"); got != "ws-123" {
		t.Errorf("unexpected workspace_id: %v", got)
	}
	if got := d.Get("customer_id"); got != "cust-123" {
		t.Errorf("unexpected customer_id: %v", got)
	}
	if got := d.Get("created_at"); got != "2026-09-17 07:51:03 +0000 UTC" {
		t.Errorf("unexpected created_at: %v", got)
	}
	if got := d.Get("updated_at"); got != "2026-09-17 08:30:00 +0000 UTC" {
		t.Errorf("unexpected updated_at: %v", got)
	}

	defList := d.Get("definition").([]interface{})
	if len(defList) != 1 {
		t.Fatalf("expected 1 definition block in state, got %d", len(defList))
	}
	definition := defList[0].(map[string]interface{})
	if got := definition["name"]; got != "pool-def" {
		t.Errorf("unexpected definition name: %v", got)
	}
	dockerList := definition["docker"].(*schema.Set).List()
	if len(dockerList) != 1 {
		t.Fatalf("expected 1 docker block in the definition, got %d", len(dockerList))
	}
	if got := dockerList[0].(map[string]interface{})["image"]; got != "koyeb/demo" {
		t.Errorf("unexpected docker image: %v", got)
	}
	instanceTypeList := definition["instance_types"].(*schema.Set).List()
	if len(instanceTypeList) != 1 {
		t.Fatalf("expected 1 instance_types block in the definition, got %d", len(instanceTypeList))
	}
	if got := instanceTypeList[0].(map[string]interface{})["type"]; got != "micro" {
		t.Errorf("unexpected instance type: %v", got)
	}
	if got := definition["regions"].(*schema.Set).List(); len(got) != 1 || got[0] != "fra" {
		t.Errorf("unexpected regions: %v", got)
	}
}
