whoami
======

This is a simple web server application that shows the client their IP address
and the GeoIP mapping to a location and ISP, if possible.

Feature
-------

### GeoIP

The app will attempt to look-up the client's location and ISP (ASN)
based on their IP address.

If you want GeoIP lookup, you'll need to point the application to a directory
on your system hosting the files from MaxMind.

The application [geoipupdate](https://dev.maxmind.com/geoip/updating-databases/)
is easy to run as cron task on your host system to keep a database up to date that apps
can share.

### Dual-Stack IP Detection

The client HTML page will, if confiured, attempt to do dual-stack IP detection.
If the client connected to the server with IPv6, JavaScript on the HTML page
will attempt an IPv4 connection to get their also get their IPv4 details. And vice-versa.

If you want the app to do dual-stack IP detection, you need to tell it what
IPv4 and IPv6 hosts clients can use to make a request on that single stack.

You could hardcode IPs, or create DNS hostnames that only resolve to one stack
and then one hostname that resolves to both. Example:

```
ip4.example.com   IN A       198.51.100.80
ip6.example.com   IN AAAA    2001:db8::80

ip.example.com    IN A       198.51.100.80
ip.example.com    IN AAAA    2001:db8::80
```

### Reverse proxy

This app can be run behind a reverse proxy. It will look for incoming headers
`x-forwarded-for` or `x-real-ip` and if one is present, it will use those IPs
instead of the client's remote address.

Running
-------

### Docker Compose

This assumes you have done the GeoIP and DNS configuration from above.

```yaml
services:
  whoami:
    container_name: whoami
    image: ghcr.io/mroach/whoami
    restart: unless-stopped
    volumes:
      # Default path used by `geoipupdate`
      - /usr/share/GeoIP:/opt/app/run/data/maxmind:ro

      # ASN logos/images are cached on disk. You could use a Docker volume for this too.
      - ./asn:/opt/cache/images/asn
    environment:
      IPV4_HOST: ip4.example.com
      IPV6_HOST: ip6.example.com
    ports:
      - 8080:8080
```

Tested Browsers
---------------

The HTML and JavaScript aim to be compatible with old browsers, especially those
that could support dual-stack IP. Tested on:

* Internet Explorer 5, 6 (Windows 2000, XP)
* Safari 3.0 (Mac OS X 10.4)
* Firefox 12.0 (Windows 2000)
* PlayStation 5
