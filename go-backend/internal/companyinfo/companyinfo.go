// Package companyinfo holds the fixed business details that appear on every
// invoice (Bill From / Shipped From / GSTIN / jurisdiction) and the
// logistics defaults applied to a new order when the caller doesn't specify
// them. The Node app kept these in an editable Settings table; this Go
// system has no settings UI yet, so they're constants here — the real
// values the user already configured on the old system.
package companyinfo

const (
	CompanyName         = "Maskyeti Solutions Pvt Ltd"
	CompanyAddress      = "Plot No-4, Sector 44 Road,Sector 44,\nGurugram\nGurugram - 122003\nHaryana (06) ,India"
	CompanyGSTIN        = "06AALCM1506H1ZO"
	CompanyStateCode    = "06" // first 2 digits of the GSTIN — decides CGST+SGST vs IGST
	ShippedFromAddress  = "2nd floor, Khasra No.20//1/1,10/2,11,21//6,15,Khewat/Khata No.155/164, Fazilpur, Jharsa, Gurgaon - 122101, Haryana (06), India"
	Jurisdiction        = "Haryana (06)"
	FulfillmentPlatform = "Unicommerce"

	DefaultOrderNo         = "#92043533346082"
	DefaultPortal          = "ASTROTALK_STORE_SHOPIFY"
	DefaultPaymentModeCode = "P1"
	DefaultPaymentMode     = "PREPAID"
	DefaultDispatchThrough = "Shipway"
	DefaultAWBNo           = "61953712159721"
)
