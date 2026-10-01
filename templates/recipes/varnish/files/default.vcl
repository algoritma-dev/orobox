vcl 4.1;

# Varnish in front of the Orobox web service. A starting point: edit it to match your cache
# policy. `orobox extend add varnish` never overwrites this file once it exists.

backend default {
    .host = "web";
    .port = "80";
}

# Who may invalidate the cache. The stack's containers live on Docker's private networks.
acl invalidators {
    "localhost";
    "10.0.0.0"/8;
    "172.16.0.0"/12;
    "192.168.0.0"/16;
}

sub vcl_recv {
    if (req.method == "PURGE") {
        if (client.ip !~ invalidators) {
            return (synth(405, "Not allowed"));
        }
        return (purge);
    }

    # FOSHttpCacheBundle bans by host, URL, content type and, when tagging is on, cache tags.
    if (req.method == "BAN") {
        if (client.ip !~ invalidators) {
            return (synth(405, "Not allowed"));
        }
        if (req.http.X-Cache-Tags) {
            ban("obj.http.X-Host ~ " + req.http.X-Host
                + " && obj.http.X-Url ~ " + req.http.X-Url
                + " && obj.http.Content-Type ~ " + req.http.X-Content-Type
                + " && obj.http.X-Cache-Tags ~ " + req.http.X-Cache-Tags);
        } else {
            ban("obj.http.X-Host ~ " + req.http.X-Host
                + " && obj.http.X-Url ~ " + req.http.X-Url
                + " && obj.http.Content-Type ~ " + req.http.X-Content-Type);
        }
        return (synth(200, "Banned"));
    }

    if (req.method != "GET" && req.method != "HEAD") {
        return (pass);
    }
    if (req.http.Authorization) {
        return (pass);
    }
}

sub vcl_backend_response {
    # Stored on the object so a BAN can match it; removed again before delivery.
    set beresp.http.X-Url = bereq.url;
    set beresp.http.X-Host = bereq.http.host;
}

sub vcl_deliver {
    unset resp.http.X-Url;
    unset resp.http.X-Host;
    unset resp.http.X-Cache-Tags;

    if (obj.hits > 0) {
        set resp.http.X-Cache = "HIT";
    } else {
        set resp.http.X-Cache = "MISS";
    }
}
