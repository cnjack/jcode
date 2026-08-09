package providerauth

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"
)

func jwtPayload(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		decoded, err = base64.URLEncoding.DecodeString(parts[1])
	}
	if err != nil {
		return nil
	}
	payload := make(map[string]any)
	if json.Unmarshal(decoded, &payload) != nil {
		return nil
	}
	return payload
}

func nestedString(value map[string]any, objectName, fieldName string) string {
	object, _ := value[objectName].(map[string]any)
	return stringField(object, fieldName)
}

func tokenExpiry(now func() time.Time, expiresIn int64) time.Time {
	if expiresIn <= 0 {
		expiresIn = 3600
	}
	if expiresIn > int64((24 * time.Hour).Seconds()) {
		expiresIn = int64((24 * time.Hour).Seconds())
	}
	return now().Add(time.Duration(expiresIn) * time.Second)
}
