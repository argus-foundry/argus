# Harden workspace proxy documents

Prevent raw daemon content from executing with the optional workspace's remembered credential, and prevent same-site loopback pages from framing its authenticated shell.

Apply a restrictive sandbox and nosniff policy to every proxied API response, preserving upstream CSP restrictions. Deny shell framing with CSP and X-Frame-Options. Keep Review's fetched blob previews and downloads available. No daemon or other frontend changes are needed because this boundary belongs to the optional Node proxy.
