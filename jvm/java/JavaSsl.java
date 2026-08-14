package io.qpoint.qtap;

import java.nio.ByteBuffer;
import javax.net.ssl.SSLEngine;
import javax.net.ssl.SSLEngineResult;

/**
 * JavaSsl - A minimal JNI wrapper for libqtap.so
 *
 * <p>This class is designed to be loaded into the bootstrap classloader and provides a safe
 * interface to the native methods for SSL interception.
 */
public class JavaSsl {
  // Load state tracking
  private static boolean loaded = false;
  private static String loadedLibPath = null;

  // ThreadLocal storage for SSLEngine correlation - simple 1:1 entry/exit pattern
  private static final ThreadLocal<java.util.HashMap<String, Object>> BUFFER_CONTEXT =
      new ThreadLocal<>();

  /** Create SSLEngine buffer context to persist context between entry and exit */
  private static java.util.HashMap<String, Object> createBufferContext(
      ByteBuffer[] srcs,
      int srcsOffset,
      int srcsLength,
      ByteBuffer[] dsts,
      int dstsOffset,
      int dstsLength) {
    // Capture source buffer positions and remaining at entry time
    int[] srcPositions = null;
    int[] srcRemaining = null;
    if (srcs != null && srcsLength > 0) {
      srcPositions = new int[srcsLength];
      srcRemaining = new int[srcsLength];
      for (int i = 0; i < srcsLength; i++) {
        ByteBuffer src = srcs[srcsOffset + i];
        if (src != null) {
          srcPositions[i] = src.position();
          srcRemaining[i] = src.remaining();
        }
      }
    }

    // Capture destination buffer positions and remaining at entry time
    int[] dstPositions = null;
    int[] dstRemaining = null;
    if (dsts != null && dstsLength > 0) {
      dstPositions = new int[dstsLength];
      dstRemaining = new int[dstsLength];
      for (int i = 0; i < dstsLength; i++) {
        ByteBuffer dst = dsts[dstsOffset + i];
        if (dst != null) {
          dstPositions[i] = dst.position();
          dstRemaining[i] = dst.remaining();
        }
      }
    }

    // Initialize context HashMap
    java.util.HashMap<String, Object> context = new java.util.HashMap<>();

    // Source buffer array reference from SSL method call
    context.put("srcs", srcs);
    // Starting index in source buffer array
    context.put("srcsOffset", Integer.valueOf(srcsOffset));
    // Number of source buffers to process
    context.put("srcsLength", Integer.valueOf(srcsLength));
    // Destination buffer array reference from SSL method call
    context.put("dsts", dsts);
    // Starting index in destination buffer array
    context.put("dstsOffset", Integer.valueOf(dstsOffset));
    // Number of destination buffers to process
    context.put("dstsLength", Integer.valueOf(dstsLength));
    // Snapshot of source buffer positions at method entry
    context.put("srcPositions", srcPositions);
    // Snapshot of source buffer remaining bytes at method entry
    context.put("srcRemaining", srcRemaining);
    // Snapshot of destination buffer positions at method entry
    context.put("dstPositions", dstPositions);
    // Snapshot of destination buffer remaining bytes at method entry
    context.put("dstRemaining", dstRemaining);

    return context;
  }

  /**
   * Load the native library
   *
   * @param libPath Absolute path to the native library
   * @return true if loaded successfully
   */
  public static synchronized boolean loadLibrary(String libPath) {
    if (!loaded) {
      try {
        System.load(libPath);
        loaded = true;
        loadedLibPath = libPath;
        return true;
      } catch (Throwable t) {
        System.err.println("Qtap: Failed to load native library: " + t.getMessage());
        return false;
      }
    }

    // If already loaded, check if it's the same path
    if (libPath.equals(loadedLibPath)) {
      return true;
    }

    return false;
  }

  /** Check if the library is loaded */
  public static boolean isLoaded() {
    return loaded;
  }

  // ---------- SSL Read/Write Interception Methods ----------

  /**
   * Called when SSL reads from a socket
   *
   * @param buffer The buffer where data will be read into
   * @param offset The offset in the buffer to start reading
   * @param length The maximum number of bytes to read
   */
  public static void safeReadEntry(byte[] buffer, int offset, int length) {
    if (!loaded) {
      return;
    }

    try {
      readEntry(buffer, offset, length);
    } catch (Throwable t) {
      // swallow the error
    }
  }

  /**
   * Called when SSL read completes
   *
   * @param bytesRead The number of bytes actually read, or -1 if EOF
   */
  public static void safeReadExit(int bytesRead) {
    if (!loaded) {
      return;
    }

    try {
      readExit(bytesRead);
    } catch (Throwable t) {
      // swallow the error
    }
  }

  /**
   * Called when SSL writes to a socket
   *
   * @param buffer The buffer containing data to write
   * @param offset The offset in the buffer to start writing
   * @param length The number of bytes to write
   */
  public static void safeWriteEntry(byte[] buffer, int offset, int length) {
    if (!loaded) {
      return;
    }

    try {
      writeEntry(buffer, offset, length);
    } catch (Throwable t) {
      // swallow the error
    }
  }

  /** Called when SSL write completes */
  public static void safeWriteExit() {
    if (!loaded) {
      return;
    }

    try {
      writeExit();
    } catch (Throwable t) {
      // swallow the error
    }
  }

  // ---------- SSLEngine Interception Methods ----------

  /** Called when SSLEngine.wrap() starts (plaintext -> encrypted) */
  public static void safeEngineWrapEntry(
      ByteBuffer[] srcs,
      int srcsOffset,
      int srcsLength,
      ByteBuffer[] dsts,
      int dstsOffset,
      int dstsLength) {
    if (!loaded) {
      return;
    }

    try {
      // Clean any stale state first (in case previous exit returned early)
      BUFFER_CONTEXT.remove();

      // Validate inputs - be more permissive to avoid missing data
      if (srcsLength < 0 || dstsLength <= 0) {
        return;
      }

      if (srcs == null || dsts == null) {
        return;
      }

      if (srcsOffset > srcs.length || dstsOffset > dsts.length) {
        return;
      }

      // Store buffer context with references and position snapshots
      java.util.HashMap<String, Object> context =
          createBufferContext(srcs, srcsOffset, srcsLength, dsts, dstsOffset, dstsLength);
      BUFFER_CONTEXT.set(context);
    } catch (Throwable t) {
      // Silently ignore errors to avoid impacting application
    }
  }

  /** Called when SSLEngine.wrap() completes */
  public static void safeEngineWrapExit(
      SSLEngine engine,
      SSLEngineResult result,
      ByteBuffer[] srcs,
      int srcsOffset,
      int srcsLength,
      ByteBuffer[] dsts,
      int dstsOffset,
      int dstsLength) {
    if (!loaded) {
      return;
    }

    try {
      // Get context from ThreadLocal
      java.util.HashMap<String, Object> context = BUFFER_CONTEXT.get();
      if (context == null) {
        return;
      }

      // Clear context immediately to prevent reuse
      BUFFER_CONTEXT.remove();

      // Extract context values
      ByteBuffer[] ctxSrcs = (ByteBuffer[]) context.get("srcs");
      int ctxSrcsOffset = ((Integer) context.get("srcsOffset")).intValue();
      int ctxSrcsLength = ((Integer) context.get("srcsLength")).intValue();
      ByteBuffer[] ctxDsts = (ByteBuffer[]) context.get("dsts");
      int ctxDstsOffset = ((Integer) context.get("dstsOffset")).intValue();
      int ctxDstsLength = ((Integer) context.get("dstsLength")).intValue();
      int[] ctxSrcPositions = (int[]) context.get("srcPositions");
      int[] ctxSrcRemaining = (int[]) context.get("srcRemaining");

      // Only skip for truly invalid conditions that would cause crashes
      if (result == null) {
        return;
      }

      // Allow zero bytesProduced - control messages are important too
      int bytesProduced = result.bytesProduced();

      // Extract session ID - use empty array if null to avoid skipping
      byte[] sessionId = new byte[0];
      if (engine != null && engine.getSession() != null && engine.getSession().getId() != null) {
        sessionId = engine.getSession().getId();
      }

      // Validate destination context
      if (ctxDstsLength <= 0 || ctxDsts == null || ctxDstsOffset >= ctxDsts.length) {
        return;
      }

      // Get the primary destination buffer (where encrypted data was written)
      ByteBuffer primaryDst = ctxDsts[ctxDstsOffset];
      if (primaryDst == null) {
        return;
      }

      // Calculate where SSL engine wrote the data - be more permissive with bounds
      int currentPos = primaryDst.position();
      int actualDataStart = Math.max(0, currentPos - bytesProduced);

      // Create a view of the encrypted data in the destination buffer
      ByteBuffer encryptedView = primaryDst.duplicate();
      encryptedView.position(actualDataStart);
      encryptedView.limit(actualDataStart + bytesProduced);

      // Make native calls for each source buffer that had data consumed
      int bytesConsumed = result.bytesConsumed();
      int consumedSoFar = 0;

      // Handle zero bytes consumed case
      if (bytesConsumed == 0) {
        // If we have encrypted output but no plaintext input consumed,
        // this could be either:
        // 1. TLS control messages (which we don't care about for plaintext capture)
        // 2. Buffered plaintext that was processed in a previous call
        //
        // Since our goal is to capture plaintext data being encrypted,
        // and bytesConsumed=0 means no new plaintext was processed,
        // we can skip these cases as they don't represent new application data.
        return;
      }

      // Send encrypted data with first buffer, then plaintext-only for subsequent buffers
      boolean isFirstBuffer = true;
      byte[] encryptedArray = null;
      int encryptedOffset = 0;
      int encryptedLength = 0;

      // Prepare encrypted data for first buffer
      if (encryptedView.hasArray()) {
        encryptedArray = encryptedView.array();
        encryptedOffset = encryptedView.arrayOffset() + encryptedView.position();
        encryptedLength = encryptedView.remaining();
      }

      // Process each buffer with consumed data
      for (int i = 0; i < ctxSrcsLength && consumedSoFar < bytesConsumed; i++) {
        ByteBuffer src = ctxSrcs[ctxSrcsOffset + i];
        if (src == null) continue;

        int srcRemaining = ctxSrcRemaining[i];
        int srcConsumed = Math.min(srcRemaining, bytesConsumed - consumedSoFar);

        // Skip buffers with no consumed data or no array access
        if (srcConsumed <= 0 || !src.hasArray()) {
          continue;
        }

        byte[] plaintextArray = src.array();
        int plaintextOffset = src.arrayOffset() + ctxSrcPositions[i];

        if (isFirstBuffer) {
          // First buffer: include encrypted data
          engineWrapExit(
              plaintextArray,
              plaintextOffset,
              srcConsumed,
              encryptedArray,
              encryptedOffset,
              encryptedLength,
              sessionId,
              sessionId.length);
          isFirstBuffer = false;
        } else {
          // Subsequent buffers: plaintext only (encrypted length = 0)
          engineWrapExit(
              plaintextArray,
              plaintextOffset,
              srcConsumed,
              null,
              0,
              0, // No encrypted data for continuation
              sessionId,
              sessionId.length);
        }

        consumedSoFar += srcConsumed;
      }
    } catch (Throwable t) {
      // Silently ignore errors to avoid impacting application
    } finally {
      // Ensure ThreadLocal is always cleaned up (in case we hit an early return)
      BUFFER_CONTEXT.remove();
    }
  }

  /** Called when SSLEngine.unwrap() starts (encrypted -> plaintext) */
  public static void safeEngineUnwrapEntry(
      ByteBuffer[] srcs,
      int srcsOffset,
      int srcsLength,
      ByteBuffer[] dsts,
      int dstsOffset,
      int dstsLength) {
    if (!loaded) {
      return;
    }

    try {
      // Clean any stale state first (in case previous exit returned early)
      BUFFER_CONTEXT.remove();

      // Validate inputs - be more permissive to avoid missing data
      if (srcsLength < 0 || dstsLength <= 0) {
        return;
      }

      if (srcs == null || dsts == null) {
        return;
      }

      if (srcsOffset > srcs.length || dstsOffset > dsts.length) {
        return;
      }

      // Store buffer context with references and position snapshots
      java.util.HashMap<String, Object> context =
          createBufferContext(srcs, srcsOffset, srcsLength, dsts, dstsOffset, dstsLength);
      BUFFER_CONTEXT.set(context);
    } catch (Throwable t) {
      // Silently ignore errors to avoid impacting application
    }
  }

  /** Called when SSLEngine.unwrap() completes */
  public static void safeEngineUnwrapExit(
      SSLEngine engine,
      SSLEngineResult result,
      ByteBuffer[] srcs,
      int srcsOffset,
      int srcsLength,
      ByteBuffer[] dsts,
      int dstsOffset,
      int dstsLength) {
    if (!loaded) {
      return;
    }

    try {
      // Get context from ThreadLocal
      java.util.HashMap<String, Object> context = BUFFER_CONTEXT.get();
      if (context == null) {
        return;
      }

      // Clear context immediately to prevent reuse
      BUFFER_CONTEXT.remove();

      // Extract context values
      ByteBuffer[] ctxSrcs = (ByteBuffer[]) context.get("srcs");
      int ctxSrcsOffset = ((Integer) context.get("srcsOffset")).intValue();
      int ctxSrcsLength = ((Integer) context.get("srcsLength")).intValue();
      ByteBuffer[] ctxDsts = (ByteBuffer[]) context.get("dsts");
      int ctxDstsOffset = ((Integer) context.get("dstsOffset")).intValue();
      int ctxDstsLength = ((Integer) context.get("dstsLength")).intValue();
      int[] ctxSrcPositions = (int[]) context.get("srcPositions");
      int[] ctxSrcRemaining = (int[]) context.get("srcRemaining");

      // Only skip for truly invalid conditions that would cause crashes
      if (result == null) {
        return;
      }

      // Allow zero bytesProduced - control messages are important too
      int bytesProduced = result.bytesProduced();

      // Extract session ID - use empty array if null to avoid skipping
      byte[] sessionId = new byte[0];
      if (engine != null && engine.getSession() != null && engine.getSession().getId() != null) {
        sessionId = engine.getSession().getId();
      }

      // Validate destination context
      if (ctxDstsLength <= 0 || ctxDsts == null || ctxDstsOffset >= ctxDsts.length) {
        return;
      }

      // Get the primary destination buffer (where plaintext data was written)
      ByteBuffer primaryDst = ctxDsts[ctxDstsOffset];
      if (primaryDst == null) {
        return;
      }

      // Calculate where SSL engine wrote the data - be more permissive with bounds
      int currentPos = primaryDst.position();
      int actualDataStart = Math.max(0, currentPos - bytesProduced);

      // Create a view of the plaintext data in the destination buffer
      ByteBuffer plaintextView = primaryDst.duplicate();
      plaintextView.position(actualDataStart);
      plaintextView.limit(actualDataStart + bytesProduced);

      // Make native calls for each source buffer that had data consumed
      int bytesConsumed = result.bytesConsumed();
      int consumedSoFar = 0;

      // Handle zero bytes consumed case
      if (bytesConsumed == 0) {
        // For zero-byte operations, process all source buffers with remaining data
        for (int i = 0; i < ctxSrcsLength; i++) {
          ByteBuffer src = ctxSrcs[ctxSrcsOffset + i];
          if (src == null) {
            continue;
          }

          int srcRemaining = ctxSrcRemaining[i];

          // Skip buffers with no remaining data or no array access
          if (srcRemaining <= 0) {
            continue;
          }
          if (!src.hasArray()) {
            continue;
          }
          if (!plaintextView.hasArray()) {
            continue;
          }

          // Extract underlying arrays with proper offsets
          byte[] encryptedArray = src.array();
          int encryptedOffset = src.arrayOffset() + ctxSrcPositions[i];
          byte[] plaintextArray = plaintextView.array();
          int plaintextOffset = plaintextView.arrayOffset() + plaintextView.position();

          // Call native method
          engineUnwrapExit(
              encryptedArray,
              encryptedOffset,
              srcRemaining,
              plaintextArray,
              plaintextOffset,
              plaintextView.remaining(),
              sessionId,
              sessionId.length);
        }
      }

      // Send plaintext data with first buffer, then encrypted-only for subsequent buffers
      boolean isFirstBuffer = true;
      byte[] plaintextArray = null;
      int plaintextOffset = 0;
      int plaintextLength = 0;

      // Prepare plaintext data for first buffer
      if (plaintextView.hasArray()) {
        plaintextArray = plaintextView.array();
        plaintextOffset = plaintextView.arrayOffset() + plaintextView.position();
        plaintextLength = plaintextView.remaining();
      }

      // Process each buffer with consumed data
      for (int i = 0; i < ctxSrcsLength && consumedSoFar < bytesConsumed; i++) {
        ByteBuffer src = ctxSrcs[ctxSrcsOffset + i];
        if (src == null) continue;

        int srcRemaining = ctxSrcRemaining[i];
        int srcConsumed = Math.min(srcRemaining, bytesConsumed - consumedSoFar);

        // Guard clause: skip buffers with no consumed data or no array access
        if (srcConsumed <= 0 || !src.hasArray()) {
          continue;
        }

        byte[] encryptedArray = src.array();
        int encryptedOffset = src.arrayOffset() + ctxSrcPositions[i];

        if (isFirstBuffer) {
          // First buffer: include plaintext data
          engineUnwrapExit(
              encryptedArray,
              encryptedOffset,
              srcConsumed,
              plaintextArray,
              plaintextOffset,
              plaintextLength,
              sessionId,
              sessionId.length);
          isFirstBuffer = false;
        } else {
          // Subsequent buffers: encrypted only (plaintext length = 0)
          engineUnwrapExit(
              encryptedArray,
              encryptedOffset,
              srcConsumed,
              null,
              0,
              0, // No plaintext data for continuation
              sessionId,
              sessionId.length);
        }

        consumedSoFar += srcConsumed;
      }
    } catch (Throwable t) {
      // Silently ignore errors to avoid impacting application
    } finally {
      // Ensure ThreadLocal is always cleaned up (in case we hit an early return)
      BUFFER_CONTEXT.remove();
    }
  }

  // ---------- Native Method Declarations ----------
  // These match the JNI function signatures in libqtap.c

  // SSLSocket native methods
  private static native void readEntry(byte[] buffer, int offset, int length);

  private static native void readExit(int result);

  private static native void writeEntry(byte[] buffer, int offset, int length);

  private static native void writeExit();

  // SSLEngine native methods
  private static native void engineWrapExit(
      byte[] plaintextArray,
      int plaintextOffset,
      int plaintextLen,
      byte[] encryptedArray,
      int encryptedOffset,
      int encryptedLen,
      byte[] sessionId,
      int sessionIdLen);

  private static native void engineUnwrapExit(
      byte[] encryptedArray,
      int encryptedOffset,
      int encryptedLen,
      byte[] plaintextArray,
      int plaintextOffset,
      int plaintextLen,
      byte[] sessionId,
      int sessionIdLen);
}
