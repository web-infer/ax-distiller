import { createClient } from "@connectrpc/connect";
import { createConnectTransport } from "@connectrpc/connect-web";

import { Inspector } from "./pb/inspector_pb";

const transport = createConnectTransport({
	baseUrl: "http://localhost:8080",
});

export const client = createClient(Inspector, transport);
