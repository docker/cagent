# TUI Attachment Support Demo

This document demonstrates how the new TUI attachment feature works in cagent.

## How to Use

In TUI mode (default), you can now attach images using the `/attach` command:

### Basic Usage

1. Start cagent in TUI mode:
   ```bash
   ./bin/cagent run examples/basic_agent.yaml
   ```

2. In the input field, type a message with the `/attach` command:
   ```
   /attach path/to/image.png Please analyze this image
   ```

3. Press Enter to send the message with the attachment.

### Supported Commands

- `/attach image.png` - Attach an image without additional text
- `/attach image.jpg Describe this photo` - Attach with descriptive text
- `Please analyze /attach diagram.svg in detail` - Attach in the middle of text
- Multi-line messages with attachments:
  ```
  Here's my question:
  /attach screenshot.png
  What's happening in this image?
  ```

### Supported Image Formats

- PNG (`.png`)
- JPEG (`.jpg`, `.jpeg`) 
- GIF (`.gif`)
- WebP (`.webp`)
- BMP (`.bmp`)
- SVG (`.svg`)

### Error Handling

If an attachment fails to load:
- The message will still be sent
- An error message will be included in the content
- The conversation continues normally

### Comparison with CLI Mode

**TUI Mode (New):**
```bash
./bin/cagent run agent.yaml
# In TUI: /attach image.png Analyze this
```

**CLI Mode (Existing):**
```bash
echo "Analyze this" | ./bin/cagent run agent.yaml --tui=false --attach image.png
# Or: echo "/attach image.png Analyze this" | ./bin/cagent run agent.yaml --tui=false
```

Both modes now support the same attachment functionality with consistent behavior.

## Implementation Details

The implementation reuses the existing attachment infrastructure:
- Same file format validation
- Same base64 encoding process
- Same multi-content message structure
- Consistent error handling between TUI and CLI modes

## Testing

Run the test suite to verify functionality:
```bash
go test ./pkg/attachment/...
```

The attachment parsing and file handling logic is thoroughly tested with various edge cases.