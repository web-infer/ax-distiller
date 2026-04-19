package cdp

type AXNodeWithRelatives struct {
	Underlying  *AXNode
	FirstChild  *AXNodeWithRelatives
	NextSibling *AXNodeWithRelatives
}
