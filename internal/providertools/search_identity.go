package providertools

// BigModelSearchMCPToolName is the only upstream tool allowed through the
// provider-managed BigModel search preset. Reserved-server ownership alone is
// insufficient because a vendor could add a side-effecting tool to the same
// endpoint in the future.
const BigModelSearchMCPToolName = "web_search_prime"

// BigModelSearchMCPServerName returns the reserved local owner name used for
// the provider-managed preset. It contains no endpoint or credential data.
func BigModelSearchMCPServerName() string {
	return bigModelSearchMCPName
}
