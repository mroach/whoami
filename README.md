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
that could support dual-stack IP.

### Internet Explorer

On [Windows 2000 with IPv6 enabled](https://mroach.com/2026/03/ipv6-on-windows-nt-4-and-2000/),
Internet Explorer 6 will still not work with IPv6. IE6 will work with IPv6 on Windows XP though.

Internet Explorer 6 has strict cross-domain constraints, so it uses JSONP instead of XHR for dual-stack detection.

Internet Explorer 5 renders fine, with no IPv6 support.

### Safari

Safari 3.0, the version that ships with Mac OS X 10.4 Tiger, supports IPv6 out of
the box, so dual-stack detection works. Safari 3 does not support `JSON`, so it will
fall-back to using the JSONP method.

Safari 4.0 ships with `JSON` support, so it can use XHR for dual-stack detection.

### Firefox

Firefox 12.0 is the newest version that runs on Windows 2000. It works out of the box
with IPv6 and XHR for dual-stack IP detection.

Firefox 52.9 ESR is the newest version that runs on Windows XP. It also works fine.

### Other working

* PlayStation 5
