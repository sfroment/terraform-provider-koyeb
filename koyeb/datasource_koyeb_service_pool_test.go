package koyeb

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
	"github.com/koyeb/koyeb-api-client-go/api/v1/koyeb"
)

// NOTE: Like TestAccKoyebServicePool_Basic, this test cannot pass a real
// TF_ACC run while the delete endpoint is not live in prod: the SDKv2
// harness runs an unconditional post-test terraform destroy and fails on
// error. It is skipped unless explicitly opted in with
// KOYEB_SERVICE_POOL_ACC=1.

func TestAccDataSourceKoyebServicePool_Basic(t *testing.T) {
	if os.Getenv("KOYEB_SERVICE_POOL_ACC") != "1" {
		t.Skip("Acceptance tests for koyeb_service_pool are opt-in (KOYEB_SERVICE_POOL_ACC=1): " +
			"the delete endpoint is not live in prod yet, so the harness's post-test destroy " +
			"would fail and leak the created pool.")
	}

	var pool koyeb.ServicePool
	poolName := randomTestName()

	resourceConfig := fmt.Sprintf(testAccKoyebServicePoolConfig_basic, poolName)

	dataSourceConfig := `
data "koyeb_service_pool" "bar" {
	name = koyeb_service_pool.foo.name
}`

	resource.ParallelTest(t, resource.TestCase{
		PreCheck:          func() { testAccPreCheck(t) },
		ProviderFactories: testAccProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: resourceConfig,
			},
			{
				Config: resourceConfig + dataSourceConfig,
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDataSourceKoyebServicePoolExists("data.koyeb_service_pool.bar", &pool),
					resource.TestCheckResourceAttr("data.koyeb_service_pool.bar", "name", poolName),
					resource.TestCheckResourceAttrSet("data.koyeb_service_pool.bar", "id"),
					resource.TestCheckResourceAttrSet("data.koyeb_service_pool.bar", "size"),
					resource.TestCheckResourceAttrSet("data.koyeb_service_pool.bar", "organization_id"),
					resource.TestCheckResourceAttrSet("data.koyeb_service_pool.bar", "ready_count"),
					resource.TestCheckResourceAttrSet("data.koyeb_service_pool.bar", "status"),
				),
			},
		},
	})
}

func testAccCheckDataSourceKoyebServicePoolExists(n string, pool *koyeb.ServicePool) resource.TestCheckFunc {
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
