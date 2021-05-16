# Go Storage

Provide storage abstraction for easily working with file object using different storage mechanism.

Storage Implementation:
- [Local Storage](#local-storage)
- [AWS S3](#aws-s3)
- [Alibaba OSS](#alibaba-oss)

## Usage

Install (go 1.15+)
```sh
go get -u github.com/commonlib-dev/go-storage
```

## Implementation

### Local Storage

If you're using local storage you need to provide root or base path where you would store files for public or private usage.

Then create endpoints to 
serve those public or private files using your http library of choice.

In order to serve private files you can create a signed request to temporarily give access to URL.

**Configuration Example using go gin:**

The complete sample source code [here](https://github.com/abdularis/go-storage-sample)

1. Create function to generate HMAC signature for a particular resource URI
```go
const secret = "this-is-your-hmac-secret"

func generateHmac(expireAt string, requestURI string) (string, error) {
	data, err := json.Marshal(map[string]interface{} {
		"expireAt": expireAt,
		"requestURI": requestURI,
	})
	if err != nil {
		return "", err
	}

	h := hmac.New(sha512.New, []byte(secret))
	h.Write(data)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil)), nil
}
```

1. Create gin middleware to check for signed URL
```go
func signedURLMiddleware(context *gin.Context) {
	expireAt := context.Query("expireAt")
	expire, err := strconv.ParseInt(expireAt, 10, 64)
	if err != nil {
		context.AbortWithStatusJSON(400, "Invalid expiration arg")
		return
	}

	if time.Now().Unix() > expire {
		context.AbortWithStatusJSON(400, "URL expired")
		return
	}

	u := context.Request.URL
	q := u.Query()

	signature := q.Get("signature")
	q.Del("signature")
	u.RawQuery = q.Encode()

	generatedSignature, err := generateHmac(expireAt, u.RequestURI())

	if generatedSignature != signature {
		context.AbortWithStatusJSON(403, "No Access Sorry Bre 🤚 ⛔")
		return
	}
}
```

3. Create signed URL builder function to be provided to local storage constructor, it is used by local storage implementation
to generate a download signed URL for particular private file object
```go
func signedURLBuilder(absoluteFilePath string, objectPath string, expireIn time.Duration) (string, error) {
	u, err := url.Parse("http://localhost:8000/private/files")
	if err != nil {
		return "", err
	}

	expireAt := fmt.Sprintf("%d", time.Now().Add(expireIn).Unix())

	q := u.Query()
	q.Add("expireAt", expireAt)

	u.Path = path.Join(u.Path, objectPath)
	u.RawQuery = q.Encode()

	signature, err := generateHmac(expireAt, u.RequestURI())
	if err != nil {
		return "", err
	}

	q.Add("signature", signature)
	u.RawQuery = q.Encode()

	return u.String(), nil
}
```

4. Instantiate storage and serve public/private files using gin router with StaticFS() route function.
```go
func main() {
	c := gin.Default()

	// Serve public files
	c.StaticFS("/files", http.Dir("storage/public"))

	// Serve private files
	c.Use(signedURLMiddleware).StaticFS("/private/files", http.Dir("storage/private"))

	storage := gostorage.NewLocalStorage(
		"storage/private",
		"storage/public",
		"http://localhost:8000/files",
		signedURLBuilder)

	dataSource := strings.NewReader("Hello, this is content 😊 😅 updated")
	_ = storage.Put("user-files/sample.txt", dataSource, gostorage.ObjectPublicRead)

	// will generate: http://localhost:8000/files/user-files/sample.txt
	publicURL, _ := storage.URL("user-files/sample.txt")
	
	// will generate: http://localhost:8000/private/files/user-files/sample.txt?expireAt=1619449697&signature=JB9d6dFOPhVLzp83EIkws2UGWMQqvnTnMGXDVY9HTZKb92TpI7K2UeocO4xgxQyhBtgeFfVMfz-NCjBB3Aeuxw
	singedURL, _ = storage.TemporaryURL("user-files/sample.txt", time.Minute)
}
```

### AWS S3

### Alibaba OSS