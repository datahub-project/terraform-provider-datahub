# Organization display preferences are a singleton, so the import ID is
# ignored -- any value brings the existing settings under management. The
# global settings URN is the clearest thing to pass:
terraform import datahub_organization_display_preferences.main urn:li:globalSettings:0

# A placeholder works identically:
terraform import datahub_organization_display_preferences.main -
