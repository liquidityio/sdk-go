package sdk

// AccountsClient wraps broker account endpoints.
type AccountsClient struct {
	c *Client
}

// PortfolioClient wraps ATS portfolio endpoints.
type PortfolioClient struct {
	c *Client
}

// OrdersClient wraps ATS order endpoints.
type OrdersClient struct {
	c *Client
}

// TradingClient wraps ATS trading control endpoints (halt/resume).
type TradingClient struct {
	c *Client
}
