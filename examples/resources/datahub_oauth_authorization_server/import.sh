# Import by URN
terraform import datahub_oauth_authorization_server.snowflake urn:li:oauthAuthorizationServer:snowflake-oauth

# Or import by bare server ID (provider constructs the URN)
terraform import datahub_oauth_authorization_server.snowflake snowflake-oauth

# NOTE: The client secret is not imported (it is encrypted server-side and
# never readable). Unlike datahub_secret, the imported server can be updated
# without re-supplying it: leave client_secret_wo and client_secret_wo_version
# unset and the stored secret is preserved. To rotate after import, set both
# the value and the version in the same change.
