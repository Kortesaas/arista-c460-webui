package main

import (
	"fmt"
	"html"
	"net/http"
	"reflect"
	"strings"
	"time"

	docassets "arista-c460-webui/docs"
)

type apiEndpoint struct{ Pattern string }

func requiredFields(pattern string) []string {
	switch pattern {
	case "POST /api/ssids", "PUT /api/ssids/{name}":
		return []string{"name", "enabled", "hidden", "opmode", "bands", "vlan", "isolation"}
	case "PUT /api/radios/{id}":
		return []string{"enabled", "channel", "width", "power", "dca", "dtp"}
	case "PUT /api/radios/{id}/wifi7":
		return []string{"enabled", "width"}
	case "PUT /api/settings":
		return []string{"siteName", "vlanNames"}
	case "PUT /api/ssh", "PUT /api/metrics":
		return []string{"enabled"}
	case "PUT /api/ssids/{name}/schedule":
		return []string{"enabled", "windows"}
	case "PUT /api/ssids/{name}/policy":
		return []string{"macFilter", "maxClients"}
	case "PUT /api/ssids/{name}/traffic":
		return []string{"bandwidth", "perClient", "qos", "clients"}
	case "PUT /api/ssids/{name}/traffic/clients/{mac}":
		return []string{"uploadKbps", "downloadKbps"}
	case "PUT /api/management":
		return []string{"mode"}
	}
	return nil
}

func validateCompleteSettings(pattern string, fields map[string]any) error {
	for _, key := range requiredFields(pattern) {
		value, present := fields[key]
		if !present || value == nil && key != "vlan" && key != "maxClients" && key != "uploadKbps" && key != "downloadKbps" && key != "qos" {
			return fmt.Errorf("Missing field %s; send the complete settings object", key)
		}
	}
	if pattern == "PUT /api/ssids/{name}/traffic" {
		for _, key := range []string{"bandwidth", "perClient"} {
			v, _ := fields[key].(map[string]any)
			if e := validateCompleteSettings("PUT /api/ssids/{name}/traffic/clients/{mac}", v); e != nil {
				return fmt.Errorf("%s: %w", key, e)
			}
		}
		clients, _ := fields["clients"].(map[string]any)
		for mac, item := range clients {
			v, _ := item.(map[string]any)
			if e := validateCompleteSettings("PUT /api/ssids/{name}/traffic/clients/{mac}", v); e != nil {
				return fmt.Errorf("%s: %w", mac, e)
			}
		}
		if fields["qos"] != nil {
			q, _ := fields["qos"].(map[string]any)
			for _, k := range []string{"priority", "mode", "mapping", "markDSCP", "mark8021p"} {
				if v, ok := q[k]; !ok || v == nil {
					return fmt.Errorf("Missing QoS field %s", k)
				}
			}
		}
	}
	if pattern == "POST /api/batch" {
		changes, _ := fields["changes"].([]any)
		for _, item := range changes {
			change, _ := item.(map[string]any)
			if kind, _ := change["kind"].(string); kind == "ssid-create" || kind == "ssid-update" {
				body, _ := change["ssid"].(map[string]any)
				if err := validateCompleteSettings("POST /api/ssids", body); err != nil {
					return err
				}
			} else if kind == "radio" {
				body, _ := change["radio"].(map[string]any)
				if err := validateCompleteSettings("PUT /api/radios/{id}", body); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Schemas come from the actual request structs, so field names stay aligned.
func jsonSchema(t reflect.Type) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	if t == reflect.TypeFor[time.Time]() {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if t.Kind() == reflect.Pointer {
		s := jsonSchema(t.Elem())
		s["nullable"] = true
		return s
	}
	s := map[string]any{}
	switch t.Kind() {
	case reflect.Bool:
		s["type"] = "boolean"
	case reflect.String:
		s["type"] = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		s["type"] = "integer"
	case reflect.Float32, reflect.Float64:
		s["type"] = "number"
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte"}
		}
		s["type"], s["items"] = "array", jsonSchema(t.Elem())
	case reflect.Map:
		s["type"], s["additionalProperties"] = "object", jsonSchema(t.Elem())
	case reflect.Struct:
		props := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			if f.Anonymous && tag == "" {
				if nested, ok := jsonSchema(f.Type)["properties"].(map[string]any); ok {
					for k, v := range nested {
						props[k] = v
					}
				}
				continue
			}
			if tag == "" {
				tag = f.Name
			}
			props[tag] = jsonSchema(f.Type)
		}
		s["type"], s["properties"] = "object", props
	}
	return s
}

func requestSchema(pattern string) map[string]any {
	var v any
	switch pattern {
	case "POST /api/login":
		v = struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}{}
	case "POST /api/password":
		v = struct {
			Current  string `json:"current"`
			Username string `json:"username"`
			Next     string `json:"next"`
		}{}
	case "PUT /api/viewer":
		v = struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}{}
	case "POST /api/tokens":
		v = tokenRequest{}
	case "POST /api/ssids", "PUT /api/ssids/{name}":
		v = ssidRequest{}
	case "PUT /api/radios/{id}":
		v = radioRequest{}
	case "PUT /api/radios/{id}/wifi7":
		v = WiFi7Settings{}
	case "PUT /api/management":
		v = ManagementRequest{}
	case "PUT /api/lldp":
		v = LLDPTiming{}
	case "PUT /api/time":
		v = TimeInput{}
	case "PUT /api/snmp":
		v = SNMPSettings{}
	case "PUT /api/ssids/{name}/schedule":
		v = SSIDSchedule{}
	case "PUT /api/ssids/{name}/policy":
		v = SSIDPolicy{}
	case "PUT /api/ssids/{name}/traffic":
		v = TrafficPolicy{}
	case "PUT /api/ssids/{name}/traffic/clients/{mac}":
		v = TrafficLimits{}
	case "POST /api/restore":
		v = restoreRequest{}
	case "POST /api/batch":
		v = struct {
			Changes []batchChange `json:"changes"`
		}{}
	case "POST /api/diagnostics":
		v = DiagnosticInput{}
	case "POST /api/captures":
		v = CaptureInput{}
	case "POST /api/support-bundle":
		v = SupportInput{}
	case "PUT /api/settings":
		v = struct {
			SiteName  string            `json:"siteName"`
			VLANNames map[string]string `json:"vlanNames"`
		}{}
	case "PUT /api/timezone":
		v = struct {
			TimeZone string `json:"timeZone"`
		}{}
	case "PUT /api/metrics":
		v = struct {
			Enabled  bool `json:"enabled"`
			NewToken bool `json:"newToken"`
		}{}
	case "PUT /api/ssh":
		v = struct {
			Enabled bool `json:"enabled"`
		}{}
	case "POST /api/locate":
		v = struct {
			Minutes int `json:"minutes"`
		}{}
	case "PUT /api/refresh":
		v = struct {
			Seconds int `json:"seconds"`
		}{}
	case "POST /api/backup":
		v = struct {
			Passphrase string `json:"passphrase"`
		}{}
	case "PUT /api/ssids/{name}/features":
		return featureSchema(ssidFeatureDefs)
	case "PUT /api/radios/{id}/features":
		return featureSchema(radioFeatureDefs)
	default:
		return map[string]any{"type": "object", "additionalProperties": false}
	}
	s := jsonSchema(reflect.TypeOf(v))
	s["additionalProperties"] = false
	if required := requiredFields(pattern); len(required) > 0 {
		s["required"] = required
	}
	props, _ := s["properties"].(map[string]any)
	set := func(key string, values map[string]any) {
		if prop, ok := props[key].(map[string]any); ok {
			for k, v := range values {
				prop[k] = v
			}
		}
	}
	switch pattern {
	case "PUT /api/ssids/{name}/traffic", "PUT /api/ssids/{name}/traffic/clients/{mac}":
		limits := func(v map[string]any) {
			v["required"] = []string{"uploadKbps", "downloadKbps"}
			v["additionalProperties"] = false
			for _, key := range []string{"uploadKbps", "downloadKbps"} {
				p := v["properties"].(map[string]any)[key].(map[string]any)
				p["minimum"] = minTrafficKbps
				p["maximum"] = maxTrafficKbps
				p["description"] = "Decimal Kbps from the wireless client's perspective; null is unlimited."
			}
		}
		if pattern == "PUT /api/ssids/{name}/traffic/clients/{mac}" {
			limits(s)
		} else {
			limits(props["bandwidth"].(map[string]any))
			limits(props["perClient"].(map[string]any))
			clients := props["clients"].(map[string]any)
			clients["maxProperties"] = maxTrafficOverrides
			limits(clients["additionalProperties"].(map[string]any))
			q := props["qos"].(map[string]any)
			q["required"] = []string{"priority", "mode", "mapping", "markDSCP", "mark8021p"}
			q["additionalProperties"] = false
			qp := q["properties"].(map[string]any)
			qp["priority"].(map[string]any)["enum"] = []string{"voice", "video", "best-effort", "background"}
			qp["mode"].(map[string]any)["enum"] = []string{"ceiling", "fixed"}
			qp["mapping"].(map[string]any)["enum"] = []string{"dscp", "8021p", "tos"}
		}
	case "POST /api/captures":
		s["required"] = []string{"interface"}
		set("protocol", map[string]any{"enum": []string{"all", "arp", "icmp", "tcp", "udp"}, "default": "all"})
		set("seconds", map[string]any{"minimum": 1, "maximum": captureMaxSeconds, "default": 10})
		set("maxBytes", map[string]any{"minimum": 65536, "maximum": captureMaxBytes, "default": 1 << 20})
		set("snapLength", map[string]any{"minimum": 64, "maximum": 4096, "default": 128})
		set("port", map[string]any{"minimum": 0, "maximum": 65535, "description": "0 disables the port filter; ARP and ICMP do not accept ports."})
		set("host", map[string]any{"description": "Optional numeric IPv4 or IPv6 address; no hostname or raw BPF expressions."})
	case "PUT /api/ssids/{name}/policy":
		set("maxClients", map[string]any{"minimum": 1, "maximum": 127})
		filter := props["macFilter"].(map[string]any)
		filter["required"] = []string{"mode", "addresses"}
		filter["additionalProperties"] = false
		fp := filter["properties"].(map[string]any)
		fp["mode"].(map[string]any)["enum"] = []string{"off", "allow", "deny"}
		fp["addresses"].(map[string]any)["maxItems"] = maxPolicyMACs
		fp["addresses"].(map[string]any)["nullable"] = false
	case "POST /api/tokens":
		set("name", map[string]any{"minLength": 1, "maxLength": 64})
		set("expiresDays", map[string]any{"minimum": 0, "maximum": 3650})
		set("scopes", map[string]any{"items": map[string]any{"type": "string", "enum": apiScopes}})
	case "POST /api/ssids", "PUT /api/ssids/{name}":
		set("opmode", map[string]any{"enum": opModes})
		set("bands", map[string]any{"items": map[string]any{"type": "string", "enum": []string{"2.4", "5", "6"}}})
		set("vlan", map[string]any{"minimum": 1, "maximum": 4094})
	case "PUT /api/radios/{id}/wifi7":
		set("width", map[string]any{"enum": []int{160, 320}})
	case "POST /api/diagnostics":
		set("tool", map[string]any{"enum": []string{"ping", "dns", "trace", "tcp"}})
		set("port", map[string]any{"minimum": 1, "maximum": 65535})
	case "POST /api/locate":
		set("minutes", map[string]any{"minimum": 1, "maximum": 30})
	case "PUT /api/lldp":
		set("interval", map[string]any{"minimum": 5, "maximum": 3600})
		set("hold", map[string]any{"minimum": 2, "maximum": 10})
	case "PUT /api/management":
		set("mode", map[string]any{"enum": []string{"static", "dhcp"}})
	}
	return s
}

func featureSchema(defs []featureDef) map[string]any {
	props := map[string]any{}
	for _, d := range defs {
		s := map[string]any{"type": "boolean", "nullable": true}
		if d.Int {
			s["type"], s["minimum"], s["maximum"] = "integer", d.Min, d.Max
		}
		props[d.Key] = s
	}
	return map[string]any{"type": "object", "properties": props, "additionalProperties": false, "description": "Omitted keys are unchanged. null resets a key to the firmware default."}
}

func responseSchema(pattern string) map[string]any {
	var v any
	switch pattern {
	case "GET /api/device":
		v = Device{}
	case "GET /api/radios":
		v = []Radio{}
	case "GET /api/ssids":
		v = []SSID{}
	case "GET /api/clients":
		v = []Client{}
	case "GET /api/neighbors":
		v = []Neighbor{}
	case "GET /api/interfaces":
		v = []Interface{}
	case "GET /api/health":
		v = []HealthItem{}
	case "GET /api/management":
		v = Management{}
	case "GET /api/state":
		v = stateResponse{}
	case "GET /api/radios/{id}":
		v = Radio{}
	case "GET /api/ssids/{name}":
		v = SSID{}
	case "GET /api/clients/{mac}":
		v = Client{}
	case "POST /api/diagnostics":
		v = DiagnosticResult{}
	case "GET /api/captures":
		v = CaptureStatus{}
	case "POST /api/captures", "GET /api/captures/{id}", "POST /api/captures/{id}/stop":
		v = CaptureJob{}
	case "POST /api/backup":
		v = Backup{}
	case "GET /api/time":
		v = TimeSettings{}
	case "GET /api/lldp":
		v = LLDPState{}
	case "GET /api/ssids/{name}/policy", "PUT /api/ssids/{name}/policy":
		v = SSIDPolicyStatus{}
	case "GET /api/ssids/{name}/traffic", "PUT /api/ssids/{name}/traffic", "PUT /api/ssids/{name}/traffic/clients/{mac}", "DELETE /api/ssids/{name}/traffic/clients/{mac}":
		v = TrafficStatus{}
	case "GET /api/snmp":
		v = snmpStatus{}
	case "GET /api/metrics":
		v = MetricsSettings{}
	case "PUT /api/radios/{id}/wifi7":
		v = WiFi7State{}
	}
	if v != nil {
		s := jsonSchema(reflect.TypeOf(v))
		if len(strings.Split(pattern, "/")) == 3 && pattern != "GET /api/state" && pattern != "GET /api/time" && pattern != "GET /api/lldp" && pattern != "GET /api/snmp" && pattern != "GET /api/metrics" && pattern != "POST /api/backup" && pattern != "POST /api/diagnostics" && pattern != "GET /api/captures" && pattern != "POST /api/captures" {
			return map[string]any{"type": "object", "properties": map[string]any{"generatedAt": map[string]any{"type": "string", "format": "date-time"}, "pollSeconds": map[string]any{"type": "integer"}, "error": map[string]any{"type": "string"}, "data": s}}
		}
		return s
	}
	return map[string]any{"description": "JSON response; see the API guide for envelopes and operation-specific results."}
}

func (a *API) specification() map[string]any {
	paths := map[string]any{}
	errorSchema := map[string]any{"type": "object", "properties": map[string]any{"error": map[string]any{"type": "string"}}}
	for _, e := range a.endpoints {
		method, path, _ := strings.Cut(e.Pattern, " ")
		v1path := strings.Replace(path, "/api/", "/api/v1/", 1)
		params := []any{}
		for _, segment := range strings.Split(path, "/") {
			if strings.HasPrefix(segment, "{") {
				params = append(params, map[string]any{"name": strings.Trim(segment, "{}"), "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
			}
		}
		if e.Pattern == "GET /api/clients" {
			for _, name := range []string{"ssid", "band"} {
				params = append(params, map[string]any{"name": name, "in": "query", "schema": map[string]any{"type": "string"}})
			}
		}
		if e.Pattern == "GET /api/history" {
			params = append(params, map[string]any{"name": "hours", "in": "query", "schema": map[string]any{"type": "number"}})
		}
		responses := map[string]any{}
		code := "200"
		if e.Pattern == "POST /api/tokens" {
			code = "201"
		}
		if e.Pattern == "POST /api/captures" {
			code = "202"
		}
		responses[code] = map[string]any{"description": "Success", "content": map[string]any{"application/json": map[string]any{"schema": responseSchema(e.Pattern)}}}
		for status, text := range map[string]string{"400": "Invalid request", "401": "Authentication required", "403": "Permission denied", "404": "Resource not found", "415": "JSON required", "429": "Too many attempts", "500": "Operation failed", "503": "AP not ready"} {
			responses[status] = map[string]any{"description": text, "content": map[string]any{"application/json": map[string]any{"schema": errorSchema}}}
		}
		op := map[string]any{"summary": method + " " + strings.TrimPrefix(path, "/api/"), "operationId": strings.ToLower(method) + "_" + strings.NewReplacer("/", "_", "{", "", "}", "", ".", "_").Replace(strings.TrimPrefix(path, "/api/")), "parameters": params, "responses": responses, "x-token-permissions": tokenPermissions(e.Pattern)}
		if e.Pattern == "GET /api/session" || e.Pattern == "POST /api/login" || e.Pattern == "POST /api/logout" {
			op["security"] = []any{}
		}
		if method == "POST" || method == "PUT" {
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": requestSchema(e.Pattern)}}}
		}
		if e.Pattern == "GET /api/docs" {
			responses[code] = map[string]any{"description": "API guide", "content": map[string]any{"text/html": map[string]any{"schema": map[string]any{"type": "string"}}}}
		}
		if e.Pattern == "GET /api/captures/{id}/download" || e.Pattern == "POST /api/support-bundle" {
			media := "application/zip"
			if e.Pattern == "GET /api/captures/{id}/download" {
				media = "application/vnd.tcpdump.pcap"
			}
			responses[code] = map[string]any{"description": "Authenticated file download", "content": map[string]any{media: map[string]any{"schema": map[string]any{"type": "string", "format": "binary"}}}}
		}
		methods, ok := paths[v1path].(map[string]any)
		if !ok {
			methods = map[string]any{}
			paths[v1path] = methods
		}
		methods[strings.ToLower(method)] = op
	}
	return map[string]any{"openapi": "3.0.3", "info": map[string]any{"title": "ARRR-ISTA C460 Local API", "version": "1.0.0", "description": "Local AP monitoring and control. Full JSON settings replace ordinary SSID/radio settings; advanced features merge specified keys. See /api/v1/docs for operational effects."}, "servers": []any{map[string]any{"url": "/"}}, "security": []any{map[string]any{"bearerAuth": []string{}}, map[string]any{"cookieAuth": []string{}}}, "components": map[string]any{"securitySchemes": map[string]any{"bearerAuth": map[string]any{"type": "http", "scheme": "bearer"}, "cookieAuth": map[string]any{"type": "apiKey", "in": "cookie", "name": sessionCookie}}}, "paths": paths}
}

func (a *API) openAPI(w http.ResponseWriter, r *http.Request) { reply(w, 200, a.specification()) }

func (a *API) apiDocs(w http.ResponseWriter, r *http.Request) {
	raw, _ := docassets.Files.ReadFile("api.md")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// Entirely local and script-free: readable on the AP without external assets.
	fmt.Fprintf(w, `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>C460 API guide</title><body style="margin:2rem auto;padding:0 1rem;max-width:1000px;color:#17233d;font:15px/1.6 system-ui"><h1>C460 Local API</h1><p><a href="/system">Back to System</a> · <a href="/api/v1/openapi.json">OpenAPI specification</a></p><pre style="white-space:pre-wrap;overflow-wrap:anywhere;font:14px/1.6 ui-monospace,monospace">%s</pre></body></html>`, html.EscapeString(string(raw)))
}
