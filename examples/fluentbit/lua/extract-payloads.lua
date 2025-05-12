-- Extract payloads from HTTP transaction logs
-- ==========================================
--
-- This Lua filter extracts payloads from HTTP transaction logs and creates
-- new records for each payload. It supports extracting payloads from the
-- request.headers, request.body, response.headers, and response.body fields.
function extract_payloads(tag, timestamp, record)
    local new_records = {}
    local fields = {
        {name = "request.headers", payload = "request_headers"},
        {name = "request.body", payload = "request_body"},
        {name = "response.headers", payload = "response_headers"},
        {name = "response.body", payload = "response_body"}
    }
    
    -- Get the request ID from the original record
    local request_id = record.request and record.request.qpoint_request_id
    
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
        local parent = nil
        local last_part = nil
        for i, part in ipairs(parts) do
            if current and current[part] then
                parent = current
                current = current[part]
                last_part = part
            else
                current = nil
                break
            end
        end
        
        -- If the field exists, create a new record and remove it from the original
        if current then
            -- Create new record for the payload
            local new_record = {
                msg = "HTTP payload",
                payload = field.payload,
                qpoint_request_id = request_id,
                data = current
            }

            -- Add the new record to the new records table, if data isn't an empty string
            if current ~= "" then
                table.insert(new_records, new_record)
            end
            
            -- Remove the field from the modified record
            if #parts == 2 then
                -- For two-level fields (e.g., request.headers)
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
