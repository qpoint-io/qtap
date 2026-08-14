#include <jni.h>
#include <stdio.h>

// -------------------- eBPF BRIDGE FUNCTIONS --------------------
// These empty functions exist solely to be hooked by eBPF probes.
// The memory barrier prevents compiler optimizations (e.g. removing the function call).

void ssl_read_entry(char *buffer, int offset, int length) {
	asm volatile("" ::: "memory");
}

void ssl_read_exit(int result) {
	asm volatile("" ::: "memory");
}

void ssl_write_entry(char *buffer, int offset, int length) {
	asm volatile("" ::: "memory");
}

void ssl_write_exit() {
	asm volatile("" ::: "memory");
}

void ssl_engine_wrap_exit(char *plaintext, int p_len, char *encrypted, int e_len, char *session_id, int id_len) {
	asm volatile("" ::: "memory");
}

void ssl_engine_unwrap_exit(char *encrypted, int e_len, char *plaintext, int p_len, char *session_id, int id_len) {
	asm volatile("" ::: "memory");
}

// -------------------- JNI FUNCTIONS --------------------
// These functions convert Java byte arrays to raw memory addresses that eBPF can read.
// We use GetPrimitiveArrayCritical for optimal performance since:
// 1. jbyteArray is an opaque Java reference that eBPF can't read directly
// 2. We need the actual memory address of the byte data
// 3. The critical section is very short (just the eBPF hook)
// 4. We don't need to make any other JNI calls while holding the reference

JNIEXPORT void JNICALL Java_io_qpoint_qtap_JavaSsl_readEntry(JNIEnv *env, jclass cls, jbyteArray buffer, jint offset, jint length) {
	// If the buffer is null or the length is 0, do nothing
	if (buffer == NULL || length <= 0) {
		return;
	}

	// Get direct access to the byte array's memory
	jbyte *bytes = (*env)->GetPrimitiveArrayCritical(env, buffer, NULL);

	// Call the eBPF bridge function
	ssl_read_entry((char *)bytes, offset, length);

	// Release the byte array
	(*env)->ReleasePrimitiveArrayCritical(env, buffer, bytes, JNI_ABORT);
}

JNIEXPORT void JNICALL Java_io_qpoint_qtap_JavaSsl_readExit(JNIEnv *env, jclass cls, jint result) {
	// Call the eBPF bridge function
	ssl_read_exit(result);
}

JNIEXPORT void JNICALL Java_io_qpoint_qtap_JavaSsl_writeEntry(JNIEnv *env, jclass cls, jbyteArray buffer, jint offset, jint length) {
	// If the buffer is null or the length is 0, do nothing
	if (buffer == NULL || length <= 0) {
		return;
	}

	// Get direct access to the byte array's memory
	jbyte *bytes = (*env)->GetPrimitiveArrayCritical(env, buffer, NULL);

	// Call the eBPF bridge function
	ssl_write_entry((char *)bytes, offset, length);

	// Release the byte array
	(*env)->ReleasePrimitiveArrayCritical(env, buffer, bytes, JNI_ABORT);
}

JNIEXPORT void JNICALL Java_io_qpoint_qtap_JavaSsl_writeExit(JNIEnv *env, jclass cls) {
	// Call the eBPF bridge function
	ssl_write_exit();
}

JNIEXPORT void JNICALL Java_io_qpoint_qtap_JavaSsl_engineWrapExit(JNIEnv *env, jclass cls, jbyteArray plaintextArray, jint plaintextOffset,
	jint plaintextLen, jbyteArray encryptedArray, jint encryptedOffset, jint encryptedLen, jbyteArray sessionId, jint sessionIdLen) {
	// Validate critical inputs - sessionId must always be present
	if (sessionId == NULL || sessionIdLen <= 0) {
		return;
	}

	// Validate that we have at least one data buffer (plaintext or encrypted)
	if ((plaintextArray == NULL || plaintextLen <= 0) && (encryptedArray == NULL || encryptedLen <= 0)) {
		return;
	}

	// Get session ID (always required)
	jbyte *sessionIdBytes = (*env)->GetPrimitiveArrayCritical(env, sessionId, NULL);
	if (sessionIdBytes == NULL) {
		return;
	}

	// Get plaintext data (if present)
	jbyte *plaintextBytes = NULL;
	if (plaintextArray != NULL && plaintextLen > 0) {
		plaintextBytes = (*env)->GetPrimitiveArrayCritical(env, plaintextArray, NULL);
		if (plaintextBytes == NULL) {
			goto cleanup_session;
		}
	}

	// Get encrypted data (if present)
	jbyte *encryptedBytes = NULL;
	if (encryptedArray != NULL && encryptedLen > 0) {
		encryptedBytes = (*env)->GetPrimitiveArrayCritical(env, encryptedArray, NULL);
		if (encryptedBytes == NULL) {
			goto cleanup_plaintext;
		}
	}

	// Ensure proper pointers (NULL for missing data)
	char *plaintextPtr = plaintextBytes ? (char *)(plaintextBytes + plaintextOffset) : NULL;
	char *encryptedPtr = encryptedBytes ? (char *)(encryptedBytes + encryptedOffset) : NULL;

	// Call the eBPF bridge function
	ssl_engine_wrap_exit(plaintextPtr, plaintextLen, encryptedPtr, encryptedLen, (char *)sessionIdBytes, sessionIdLen);

	// Cleanup - release resources in reverse order of acquisition
	if (encryptedBytes)
		(*env)->ReleasePrimitiveArrayCritical(env, encryptedArray, encryptedBytes, JNI_ABORT);

cleanup_plaintext:
	if (plaintextBytes)
		(*env)->ReleasePrimitiveArrayCritical(env, plaintextArray, plaintextBytes, JNI_ABORT);

cleanup_session:
	(*env)->ReleasePrimitiveArrayCritical(env, sessionId, sessionIdBytes, JNI_ABORT);
}

JNIEXPORT void JNICALL Java_io_qpoint_qtap_JavaSsl_engineUnwrapExit(JNIEnv *env, jclass cls, jbyteArray encryptedArray, jint encryptedOffset,
	jint encryptedLen, jbyteArray plaintextArray, jint plaintextOffset, jint plaintextLen, jbyteArray sessionId, jint sessionIdLen) {
	// Validate critical inputs - sessionId must always be present
	if (sessionId == NULL || sessionIdLen <= 0) {
		return;
	}

	// Validate that we have at least one data buffer (encrypted or plaintext)
	if ((encryptedArray == NULL || encryptedLen <= 0) && (plaintextArray == NULL || plaintextLen <= 0)) {
		return;
	}

	// Get session ID (always required)
	jbyte *sessionIdBytes = (*env)->GetPrimitiveArrayCritical(env, sessionId, NULL);
	if (sessionIdBytes == NULL) {
		return;
	}

	// Get encrypted data (if present)
	jbyte *encryptedBytes = NULL;
	if (encryptedArray != NULL && encryptedLen > 0) {
		encryptedBytes = (*env)->GetPrimitiveArrayCritical(env, encryptedArray, NULL);
		if (encryptedBytes == NULL) {
			goto cleanup_session;
		}
	}

	// Get plaintext data (if present)
	jbyte *plaintextBytes = NULL;
	if (plaintextArray != NULL && plaintextLen > 0) {
		plaintextBytes = (*env)->GetPrimitiveArrayCritical(env, plaintextArray, NULL);
		if (plaintextBytes == NULL) {
			goto cleanup_encrypted;
		}
	}

	// Ensure proper pointers (NULL for missing data)
	char *encryptedPtr = encryptedBytes ? (char *)(encryptedBytes + encryptedOffset) : NULL;
	char *plaintextPtr = plaintextBytes ? (char *)(plaintextBytes + plaintextOffset) : NULL;

	// Call the eBPF bridge function
	ssl_engine_unwrap_exit(encryptedPtr, encryptedLen, plaintextPtr, plaintextLen, (char *)sessionIdBytes, sessionIdLen);

	// Cleanup - release resources in reverse order of acquisition
	if (plaintextBytes)
		(*env)->ReleasePrimitiveArrayCritical(env, plaintextArray, plaintextBytes, JNI_ABORT);

cleanup_encrypted:
	if (encryptedBytes)
		(*env)->ReleasePrimitiveArrayCritical(env, encryptedArray, encryptedBytes, JNI_ABORT);

cleanup_session:
	(*env)->ReleasePrimitiveArrayCritical(env, sessionId, sessionIdBytes, JNI_ABORT);
}
