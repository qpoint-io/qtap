-- S3 Payload Handler for HTTP Transaction Logs
-- ==========================================
--
-- This Lua filter processes HTTP transaction logs and:
-- 1. Extracts payloads from request/response bodies and headers
-- 2. Generates full S3 URLs for each payload
-- 3. Adds the URLs to the original record
-- 4. Creates separate records for S3 upload

function process_payloads(tag, timestamp, record)
    local new_records = {}
    local fields = {
        {name = "request.headers", payload = "request_headers", url_field = "request_headers_url"},
        {name = "request.body", payload = "request_body", url_field = "request_body_url"},
        {name = "response.headers", payload = "response_headers", url_field = "response_headers_url"},
        {name = "response.body", payload = "response_body", url_field = "response_body_url"}
    }
    
    -- Get the request ID from the original record
    local request_id = record.request and record.request.qpoint_request_id
    
    -- Get S3 configuration from environment variables
    local s3_bucket = os.getenv("S3_BUCKET")
    local s3_region = os.getenv("S3_REGION")
    local s3_endpoint = os.getenv("S3_ENDPOINT")
    
    -- Determine the base URL for S3 objects
    local base_url
    if s3_endpoint then
        -- Use custom endpoint if provided
        base_url = string.format("%s/%s", s3_endpoint, s3_bucket)
    else
        -- Use standard AWS S3 URL format
        base_url = string.format("https://%s.s3.%s.amazonaws.com", s3_bucket, s3_region)
    end
    
    -- Create a copy of the original record to modify
    local modified_record = {}
    for k, v in pairs(record) do
        modified_record[k] = v
    end
    
    -- Check each field and create new records if they exist
    for _, field in ipairs(fields) do
        local value = record
        local parts = {}
        for part in string.gmatch(field.name, "[^.]+") do
            table.insert(parts, part)
        end
        
        -- Navigate to the field value
        local current = value
        for i, part in ipairs(parts) do
            if current and current[part] then
                current = current[part]
            else
                current = nil
                break
            end
        end
        
        -- If the field exists and isn't empty, create a new record and add URL to original
        if current and current ~= "" then
            -- Generate S3 key
            local s3_key = string.format("%s/%s.json", request_id, field.payload)
            
            -- Generate full S3 URL
            local s3_url = string.format("%s/%s", base_url, s3_key)
            
            -- Create new record for the payload
            local new_record = {
                msg = "HTTP payload",
                payload = field.payload,
                qpoint_request_id = request_id,
                data = current,
                s3_key = s3_key
            }

            -- Add the new record to the new records table
            table.insert(new_records, new_record)
            
            -- Add S3 URL to the modified record
            if #parts == 2 then
                if not modified_record[parts[1]] then
                    modified_record[parts[1]] = {}
                end
                modified_record[parts[1]][field.url_field] = s3_url
            end
            
            -- Remove the original field from the modified record
            if #parts == 2 then
                if modified_record[parts[1]] then
                    modified_record[parts[1]][parts[2]] = nil
                end
            end
        end
    end
    
    -- Add the modified original record to the new records
    table.insert(new_records, 1, modified_record)
    
    -- Return all records
    return 2, timestamp, new_records
end
