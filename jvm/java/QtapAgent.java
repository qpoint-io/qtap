package io.qpoint.qtap;

import java.io.File;
import java.lang.instrument.ClassFileTransformer;
import java.lang.instrument.Instrumentation;
import java.security.ProtectionDomain;
import java.util.jar.JarFile;
import org.objectweb.asm.ClassReader;
import org.objectweb.asm.ClassVisitor;
import org.objectweb.asm.ClassWriter;
import org.objectweb.asm.Label;
import org.objectweb.asm.MethodVisitor;
import org.objectweb.asm.Opcodes;
import org.objectweb.asm.Type;
import org.objectweb.asm.commons.AdviceAdapter;

/**
 * QtapAgent - Java Agent for SSL/TLS Traffic Interception
 *
 * <p>This agent uses bytecode instrumentation to intercept SSL/TLS traffic in Java applications.
 * The overall architecture works as follows:
 *
 * <p>1. Architecture Overview: - Java Agent: Loaded into the JVM to perform bytecode
 * instrumentation - Native Library (libqtap.so): Provides bridge functions to eBPF - eBPF Program:
 * Captures and processes the SSL/TLS traffic
 *
 * <p>2. Instrumentation Strategy: - Uses ASM (bytecode manipulation library) to modify SSL classes
 * at runtime - Targets specific SSL implementation classes (javax.net.ssl.SSLSocket streams) -
 * Intercepts read() and write() methods to capture encrypted data
 *
 * <p>3. ASM Implementation: - ClassFileTransformer identifies target SSL classes - ClassVisitor
 * finds target methods (read/write) - MethodVisitor/AdviceAdapter inserts code at method entry/exit
 * points - Try-catch blocks ensure instrumentation doesn't break application
 *
 * <p>4. Data Flow: - Intercepted SSL data is passed to JavaSsl utility class - JavaSsl calls JNI
 * methods in libqtap.so - libqtap.so calls empty bridge functions that eBPF hooks - eBPF captures
 * and processes the SSL/TLS traffic
 *
 * <p>This approach allows for minimal overhead while providing visibility into encrypted traffic
 * without modifying application code or breaking encryption.
 */
public class QtapAgent {

  // Agent agentmain method - for dynamic attach
  public static void agentmain(String args, Instrumentation inst) {
    // Validate the directory path from args
    String dirPath = validateDirPath(args);

    // Construct file paths for helper JAR and native lib
    String helperJarPath = dirPath + File.separator + "java-ssl.jar";
    String libPath = dirPath + File.separator + "libqtap.so";

    // Add helper JAR to bootstrap classloader for visibility to SSL classes
    try {
      File helperJarFile = new File(helperJarPath);
      if (!helperJarFile.exists()) {
        System.err.println("Qtap: Helper JAR not found at: " + helperJarPath);
        System.err.println("Qtap: Native interception will not work without java-ssl.jar");
      } else {
        JarFile jar = new JarFile(helperJarFile);
        inst.appendToBootstrapClassLoaderSearch(jar);
        System.out.println(
            "Qtap: Added helper JAR to bootstrap classloader: " + helperJarFile.getAbsolutePath());
      }
    } catch (Exception e) {
      System.err.println("Qtap: Error adding helper JAR to bootstrap classloader: " + e);
      e.printStackTrace();
    }

    // Load the native library and install transformer
    installTransformer(inst, libPath);
  }

  private static String validateDirPath(String args) {
    if (args == null || args.trim().isEmpty()) {
      throw new IllegalArgumentException("Directory path must be specified in agent arguments");
    }
    String dirPath = args.trim();
    File dir = new File(dirPath);
    if (!dir.exists() || !dir.isDirectory()) {
      System.err.println(
          "Qtap: WARNING: Directory does not exist or is not a directory: " + dirPath);
      // Continue anyway as we'll check for the specific files
    }
    return dirPath;
  }

  /**
   * Install the targeted SSL interceptor with the given native library path
   *
   * <p>This method attempts to load the native library using the JavaSsl class. If successful, it
   * adds the SSLClassTransformer as a bytecode transformer to the JVM.
   *
   * <p>This works by: 1. Attempting to load the native library using JavaSsl 2. If successful,
   * adding the SSLClassTransformer as a bytecode transformer to the JVM 3. Requesting
   * retransformation for our target classes
   *
   * @param inst The Instrumentation instance
   * @param libPath The path to the native library
   */
  private static void installTransformer(Instrumentation inst, String libPath) {
    System.out.println("Qtap: Installing SSL hooks with libPath: " + libPath);

    try {
      // Try to load the library using JavaSsl
      try {
        // First check if JavaSsl class is accessible
        Class.forName("io.qpoint.qtap.JavaSsl");

        // Use reflection to avoid direct compile-time dependencies
        // This allows our agent to work even if JavaSsl can't be loaded
        Class<?> javaSslClass = Class.forName("io.qpoint.qtap.JavaSsl");
        java.lang.reflect.Method loadMethod = javaSslClass.getMethod("loadLibrary", String.class);
        boolean loaded = (Boolean) loadMethod.invoke(null, libPath);

        if (loaded) {
          System.out.println("Qtap: Native library loaded successfully via JavaSsl");
        } else {
          System.err.println("Qtap: Failed to load native library via JavaSsl");
        }
      } catch (ClassNotFoundException e) {
        System.err.println("Qtap: JavaSsl class not found");
      } catch (Exception e) {
        System.err.println("Qtap: Error loading native library via JavaSsl: " + e);
        e.printStackTrace();
      }
    } catch (Exception e) {
      System.err.println("Qtap: Failed to load native library: " + e);
      e.printStackTrace();
    }

    // Add our direct bytecode transformer
    inst.addTransformer(new SSLClassTransformer(), true);

    // Now, explicitly request retransformation for our target classes
    try {
      Class<?>[] loadedClasses = inst.getAllLoadedClasses();

      for (Class<?> clazz : loadedClasses) {
        if (clazz.getName().equals("sun.security.ssl.SSLSocketImpl$AppInputStream")
            || clazz.getName().equals("sun.security.ssl.SSLSocketImpl$AppOutputStream")
            || clazz.getName().equals("sun.security.ssl.SSLEngineImpl")) {

          if (inst.isModifiableClass(clazz)) {
            System.out.println("Qtap: Requesting entry/exit hooks for: " + clazz.getName());
            inst.retransformClasses(clazz);
          } else {
            System.out.println("Qtap: Class is not modifiable: " + clazz.getName());
          }
        }
      }

      // System.out.println("Qtap: SSL hooks installed - waiting for SSL socket activity");
    } catch (Exception e) {
      System.err.println("Qtap: Error during asm hooks: " + e);
      e.printStackTrace();
    }
  }

  private static class SSLClassTransformer implements ClassFileTransformer {
    @Override
    public byte[] transform(
        ClassLoader loader,
        String className,
        Class<?> classBeingRedefined,
        ProtectionDomain protectionDomain,
        byte[] classfileBuffer) {

      // Only transform classes we're interested in
      if (className == null) {
        return null;
      }

      try {
        if (className.equals("sun/security/ssl/SSLSocketImpl$AppInputStream")) {
          System.out.println("Qtap: Adding safe observer to AppInputStream");
          return transformInputStream(classfileBuffer);
        } else if (className.equals("sun/security/ssl/SSLSocketImpl$AppOutputStream")) {
          System.out.println("Qtap: Adding safe observer to AppOutputStream");
          return transformOutputStream(classfileBuffer);
        } else if (className.equals("sun/security/ssl/SSLEngineImpl")) {
          System.out.println("Qtap: Adding safe observer to SSLEngineImpl");
          return transformSSLEngine(classfileBuffer);
        }
      } catch (Exception e) {
        System.err.println("Qtap: Error adding entry/exit hooks for " + className + ": " + e);
        e.printStackTrace();
      }

      return null; // No transformation
    }

    private byte[] transformInputStream(byte[] classBytes) {
      try {
        ClassReader cr = new ClassReader(classBytes);
        ClassWriter cw = new ClassWriter(cr, ClassWriter.COMPUTE_MAXS | ClassWriter.COMPUTE_FRAMES);
        ClassVisitor cv = new SSLInputStreamVisitor(Opcodes.ASM9, cw);

        cr.accept(cv, ClassReader.EXPAND_FRAMES);
        return cw.toByteArray();
      } catch (Exception e) {
        System.err.println("Qtap: Error adding entry/exit hooks for input stream: " + e);
        e.printStackTrace();
        return null;
      }
    }

    private byte[] transformOutputStream(byte[] classBytes) {
      try {
        ClassReader cr = new ClassReader(classBytes);
        ClassWriter cw = new ClassWriter(cr, ClassWriter.COMPUTE_MAXS | ClassWriter.COMPUTE_FRAMES);
        ClassVisitor cv = new SSLOutputStreamVisitor(Opcodes.ASM9, cw);

        cr.accept(cv, ClassReader.EXPAND_FRAMES);
        return cw.toByteArray();
      } catch (Exception e) {
        System.err.println("Qtap: Error adding entry/exit hooks for output stream: " + e);
        e.printStackTrace();
        return null;
      }
    }

    private byte[] transformSSLEngine(byte[] classBytes) {
      try {
        ClassReader cr = new ClassReader(classBytes);
        ClassWriter cw = new ClassWriter(cr, ClassWriter.COMPUTE_MAXS | ClassWriter.COMPUTE_FRAMES);
        ClassVisitor cv = new SSLEngineVisitor(Opcodes.ASM9, cw);

        cr.accept(cv, ClassReader.EXPAND_FRAMES);
        return cw.toByteArray();
      } catch (Exception e) {
        System.err.println("Qtap: Error adding entry/exit hooks for SSLEngine: " + e);
        e.printStackTrace();
        return null;
      }
    }
  }

  private static class SSLInputStreamVisitor extends ClassVisitor {
    public SSLInputStreamVisitor(int api, ClassVisitor cv) {
      super(api, cv);
    }

    @Override
    public MethodVisitor visitMethod(
        int access, String name, String descriptor, String signature, String[] exceptions) {
      MethodVisitor mv = super.visitMethod(access, name, descriptor, signature, exceptions);

      // Match the read(byte[], int, int) method
      if (name.equals("read") && descriptor.equals("([BII)I")) {
        // System.out.println("Qtap: Found read([BII)I method");
        return new ReadMethodAdapter(Opcodes.ASM9, mv, access, name, descriptor);
      }

      return mv;
    }
  }

  private static class SSLOutputStreamVisitor extends ClassVisitor {
    public SSLOutputStreamVisitor(int api, ClassVisitor cv) {
      super(api, cv);
    }

    @Override
    public MethodVisitor visitMethod(
        int access, String name, String descriptor, String signature, String[] exceptions) {
      MethodVisitor mv = super.visitMethod(access, name, descriptor, signature, exceptions);

      // Match the write(byte[], int, int) method
      if (name.equals("write") && descriptor.equals("([BII)V")) {
        // System.out.println("Qtap: Found write([BII)V method");
        return new WriteMethodAdapter(Opcodes.ASM9, mv, access, name, descriptor);
      }

      return mv;
    }
  }

  private static class SSLEngineVisitor extends ClassVisitor {
    public SSLEngineVisitor(int api, ClassVisitor cv) {
      super(api, cv);
    }

    @Override
    public MethodVisitor visitMethod(
        int access, String name, String descriptor, String signature, String[] exceptions) {
      MethodVisitor mv = super.visitMethod(access, name, descriptor, signature, exceptions);

      // Match the wrap and unwrap methods
      // wrap(ByteBuffer[], int, int, ByteBuffer[], int, int) -> SSLEngineResult
      // unwrap(ByteBuffer[], int, int, ByteBuffer[], int, int) -> SSLEngineResult
      if ((name.equals("wrap") || name.equals("unwrap"))
          && descriptor.equals(
              "([Ljava/nio/ByteBuffer;II[Ljava/nio/ByteBuffer;II)Ljavax/net/ssl/SSLEngineResult;")) {
        // System.out.println("Qtap: Found SSLEngine " + name + " method");
        if (name.equals("wrap")) {
          return new EngineWrapMethodAdapter(Opcodes.ASM9, mv, access, name, descriptor);
        } else {
          return new EngineUnwrapMethodAdapter(Opcodes.ASM9, mv, access, name, descriptor);
        }
      }

      return mv;
    }
  }

  /** Safe implementation of ReadMethodAdapter */
  private static class ReadMethodAdapter extends AdviceAdapter {
    protected ReadMethodAdapter(
        int api, MethodVisitor mv, int access, String name, String descriptor) {
      super(api, mv, access, name, descriptor);
    }

    @Override
    protected void onMethodEnter() {
      // Create labels for try-catch blocks
      Label tryStart = new Label();
      Label tryEnd = new Label();
      Label catchHandler = new Label();
      Label continueExecution = new Label();

      // Add a try-catch block around our instrumentation
      mv.visitTryCatchBlock(tryStart, tryEnd, catchHandler, "java/lang/Throwable");

      // Start of protected region
      mv.visitLabel(tryStart);

      // Call JavaSsl method - no longer trying direct native calls
      mv.visitVarInsn(ALOAD, 1); // byte[] buffer
      mv.visitVarInsn(ILOAD, 2); // offset
      mv.visitVarInsn(ILOAD, 3); // length
      mv.visitMethodInsn(INVOKESTATIC, "io/qpoint/qtap/JavaSsl", "safeReadEntry", "([BII)V", false);

      // End of protected region
      mv.visitLabel(tryEnd);
      mv.visitJumpInsn(GOTO, continueExecution); // Jump over the exception handler

      // Exception handler - catch any exceptions from our instrumentation
      mv.visitLabel(catchHandler);
      int exceptionVar = newLocal(org.objectweb.asm.Type.getType(Throwable.class));
      mv.visitVarInsn(ASTORE, exceptionVar);

      // Continue with the original method
      mv.visitLabel(continueExecution);
    }

    @Override
    protected void onMethodExit(int opcode) {
      // Early return for exception cases
      if (opcode == ATHROW) {
        return;
      }

      // Declare resultVar at the method level so it's accessible in the exception handler
      int resultVar = -1;

      // Create labels for try-catch blocks
      Label tryStart = new Label();
      Label tryEnd = new Label();
      Label catchHandler = new Label();
      Label continueExecution = new Label();

      // Add a try-catch block around our exit instrumentation
      mv.visitTryCatchBlock(tryStart, tryEnd, catchHandler, "java/lang/Throwable");

      // Start of protected region
      mv.visitLabel(tryStart);

      // For IRETURN (integer return from read method)
      if (opcode == IRETURN) {
        // Store the return value temporarily
        resultVar = newLocal(org.objectweb.asm.Type.INT_TYPE);
        mv.visitVarInsn(ISTORE, resultVar);

        // Call JavaSsl readExit method
        mv.visitVarInsn(ILOAD, resultVar);
        mv.visitMethodInsn(INVOKESTATIC, "io/qpoint/qtap/JavaSsl", "safeReadExit", "(I)V", false);

        // Put the return value back on the stack
        mv.visitVarInsn(ILOAD, resultVar);
      }

      // End of protected region
      mv.visitLabel(tryEnd);
      mv.visitJumpInsn(GOTO, continueExecution); // Jump over the exception handler

      // Exception handler
      mv.visitLabel(catchHandler);
      int exceptionVar = newLocal(org.objectweb.asm.Type.getType(Throwable.class));
      mv.visitVarInsn(ASTORE, exceptionVar);

      // If we were handling IRETURN and resultVar was initialized, put the value back
      if (opcode == IRETURN && resultVar >= 0) {
        mv.visitVarInsn(ILOAD, resultVar);
      }

      // Continue returning
      mv.visitLabel(continueExecution);
    }
  }

  /** Safe implementation of WriteMethodAdapter */
  private static class WriteMethodAdapter extends AdviceAdapter {
    protected WriteMethodAdapter(
        int api, MethodVisitor mv, int access, String name, String descriptor) {
      super(api, mv, access, name, descriptor);
    }

    @Override
    protected void onMethodEnter() {
      // Create labels for try-catch blocks
      Label tryStart = new Label();
      Label tryEnd = new Label();
      Label catchHandler = new Label();
      Label continueExecution = new Label();

      // Add a try-catch block around our instrumentation
      mv.visitTryCatchBlock(tryStart, tryEnd, catchHandler, "java/lang/Throwable");

      // Start of protected region
      mv.visitLabel(tryStart);

      // Call JavaSsl method - no longer trying direct native calls
      mv.visitVarInsn(ALOAD, 1); // byte[] buffer
      mv.visitVarInsn(ILOAD, 2); // offset
      mv.visitVarInsn(ILOAD, 3); // length
      mv.visitMethodInsn(
          INVOKESTATIC, "io/qpoint/qtap/JavaSsl", "safeWriteEntry", "([BII)V", false);

      // End of protected region
      mv.visitLabel(tryEnd);
      mv.visitJumpInsn(GOTO, continueExecution); // Jump over the exception handler

      // Exception handler
      mv.visitLabel(catchHandler);
      int exceptionVar = newLocal(org.objectweb.asm.Type.getType(Throwable.class));
      mv.visitVarInsn(ASTORE, exceptionVar);

      // Continue with the original method
      mv.visitLabel(continueExecution);
    }

    @Override
    protected void onMethodExit(int opcode) {
      // Early return for exception cases
      if (opcode == ATHROW) {
        return;
      }

      // Create labels for try-catch blocks
      Label tryStart = new Label();
      Label tryEnd = new Label();
      Label catchHandler = new Label();
      Label continueExecution = new Label();

      // Add a try-catch block around our exit instrumentation
      mv.visitTryCatchBlock(tryStart, tryEnd, catchHandler, "java/lang/Throwable");

      // Start of protected region
      mv.visitLabel(tryStart);

      // Call JavaSsl writeExit method
      mv.visitMethodInsn(INVOKESTATIC, "io/qpoint/qtap/JavaSsl", "safeWriteExit", "()V", false);

      // End of protected region
      mv.visitLabel(tryEnd);
      mv.visitJumpInsn(GOTO, continueExecution); // Jump over the exception handler

      // Exception handler
      mv.visitLabel(catchHandler);
      int exceptionVar = newLocal(org.objectweb.asm.Type.getType(Throwable.class));
      mv.visitVarInsn(ASTORE, exceptionVar);

      // Continue returning
      mv.visitLabel(continueExecution);
    }
  }

  /** Method adapter for SSLEngine.wrap() operations */
  private static class EngineWrapMethodAdapter extends AdviceAdapter {
    protected EngineWrapMethodAdapter(
        int api, MethodVisitor mv, int access, String name, String descriptor) {
      super(api, mv, access, name, descriptor);
    }

    @Override
    protected void onMethodEnter() {
      // Create labels for try-catch blocks
      Label tryStart = new Label();
      Label tryEnd = new Label();
      Label catchHandler = new Label();
      Label continueExecution = new Label();

      // Add a try-catch block around our instrumentation
      mv.visitTryCatchBlock(tryStart, tryEnd, catchHandler, "java/lang/Throwable");

      // Start of protected region
      mv.visitLabel(tryStart);

      // Call JavaSsl.safeEngineWrapEntry(srcs, srcsOffset, srcsLength, dsts, dstsOffset,
      // dstsLength)
      mv.visitVarInsn(ALOAD, 1); // ByteBuffer[] srcs
      mv.visitVarInsn(ILOAD, 2); // int srcsOffset
      mv.visitVarInsn(ILOAD, 3); // int srcsLength
      mv.visitVarInsn(ALOAD, 4); // ByteBuffer[] dsts
      mv.visitVarInsn(ILOAD, 5); // int dstsOffset
      mv.visitVarInsn(ILOAD, 6); // int dstsLength
      mv.visitMethodInsn(
          INVOKESTATIC,
          "io/qpoint/qtap/JavaSsl",
          "safeEngineWrapEntry",
          "([Ljava/nio/ByteBuffer;II[Ljava/nio/ByteBuffer;II)V",
          false);

      // End of protected region
      mv.visitLabel(tryEnd);
      mv.visitJumpInsn(GOTO, continueExecution);

      // Exception handler
      mv.visitLabel(catchHandler);
      int exceptionVar = newLocal(Type.getType(Throwable.class));
      mv.visitVarInsn(ASTORE, exceptionVar);

      // Continue with the original method
      mv.visitLabel(continueExecution);
    }

    @Override
    protected void onMethodExit(int opcode) {
      // Early return for exception cases
      if (opcode == ATHROW) {
        return;
      }

      int resultVar = -1;

      // Create labels for try-catch blocks
      Label tryStart = new Label();
      Label tryEnd = new Label();
      Label catchHandler = new Label();
      Label continueExecution = new Label();

      // Add a try-catch block around our exit instrumentation
      mv.visitTryCatchBlock(tryStart, tryEnd, catchHandler, "java/lang/Throwable");

      // Start of protected region
      mv.visitLabel(tryStart);

      // For ARETURN (SSLEngineResult return)
      if (opcode == ARETURN) {
        // Store the return value temporarily
        resultVar = newLocal(Type.getType("Ljavax/net/ssl/SSLEngineResult;"));
        mv.visitVarInsn(ASTORE, resultVar);

        // Call JavaSsl.safeEngineWrapExit with all parameters and result
        mv.visitVarInsn(ALOAD, 0); // SSLEngine this
        mv.visitVarInsn(ALOAD, resultVar); // SSLEngineResult result
        mv.visitVarInsn(ALOAD, 1); // ByteBuffer[] srcs
        mv.visitVarInsn(ILOAD, 2); // int srcsOffset
        mv.visitVarInsn(ILOAD, 3); // int srcsLength
        mv.visitVarInsn(ALOAD, 4); // ByteBuffer[] dsts
        mv.visitVarInsn(ILOAD, 5); // int dstsOffset
        mv.visitVarInsn(ILOAD, 6); // int dstsLength
        mv.visitMethodInsn(
            INVOKESTATIC,
            "io/qpoint/qtap/JavaSsl",
            "safeEngineWrapExit",
            "(Ljavax/net/ssl/SSLEngine;Ljavax/net/ssl/SSLEngineResult;[Ljava/nio/ByteBuffer;II[Ljava/nio/ByteBuffer;II)V",
            false);

        // Put the return value back on the stack
        mv.visitVarInsn(ALOAD, resultVar);
      }

      // End of protected region
      mv.visitLabel(tryEnd);
      mv.visitJumpInsn(GOTO, continueExecution);

      // Exception handler
      mv.visitLabel(catchHandler);
      int exceptionVar = newLocal(Type.getType(Throwable.class));
      mv.visitVarInsn(ASTORE, exceptionVar);

      // If we were handling ARETURN and resultVar was initialized, put the value back
      if (opcode == ARETURN && resultVar >= 0) {
        mv.visitVarInsn(ALOAD, resultVar);
      }

      // Continue returning
      mv.visitLabel(continueExecution);
    }
  }

  /** Method adapter for SSLEngine.unwrap() operations */
  private static class EngineUnwrapMethodAdapter extends AdviceAdapter {
    protected EngineUnwrapMethodAdapter(
        int api, MethodVisitor mv, int access, String name, String descriptor) {
      super(api, mv, access, name, descriptor);
    }

    @Override
    protected void onMethodEnter() {
      // Create labels for try-catch blocks
      Label tryStart = new Label();
      Label tryEnd = new Label();
      Label catchHandler = new Label();
      Label continueExecution = new Label();

      // Add a try-catch block around our instrumentation
      mv.visitTryCatchBlock(tryStart, tryEnd, catchHandler, "java/lang/Throwable");

      // Start of protected region
      mv.visitLabel(tryStart);

      // Call JavaSsl.safeEngineUnwrapEntry(srcs, srcsOffset, srcsLength, dsts, dstsOffset,
      // dstsLength)
      mv.visitVarInsn(ALOAD, 1); // ByteBuffer[] srcs
      mv.visitVarInsn(ILOAD, 2); // int srcsOffset
      mv.visitVarInsn(ILOAD, 3); // int srcsLength
      mv.visitVarInsn(ALOAD, 4); // ByteBuffer[] dsts
      mv.visitVarInsn(ILOAD, 5); // int dstsOffset
      mv.visitVarInsn(ILOAD, 6); // int dstsLength
      mv.visitMethodInsn(
          INVOKESTATIC,
          "io/qpoint/qtap/JavaSsl",
          "safeEngineUnwrapEntry",
          "([Ljava/nio/ByteBuffer;II[Ljava/nio/ByteBuffer;II)V",
          false);

      // End of protected region
      mv.visitLabel(tryEnd);
      mv.visitJumpInsn(GOTO, continueExecution);

      // Exception handler
      mv.visitLabel(catchHandler);
      int exceptionVar = newLocal(Type.getType(Throwable.class));
      mv.visitVarInsn(ASTORE, exceptionVar);

      // Continue with the original method
      mv.visitLabel(continueExecution);
    }

    @Override
    protected void onMethodExit(int opcode) {
      // Early return for exception cases
      if (opcode == ATHROW) {
        return;
      }

      int resultVar = -1;

      // Create labels for try-catch blocks
      Label tryStart = new Label();
      Label tryEnd = new Label();
      Label catchHandler = new Label();
      Label continueExecution = new Label();

      // Add a try-catch block around our exit instrumentation
      mv.visitTryCatchBlock(tryStart, tryEnd, catchHandler, "java/lang/Throwable");

      // Start of protected region
      mv.visitLabel(tryStart);

      // For ARETURN (SSLEngineResult return)
      if (opcode == ARETURN) {
        // Store the return value temporarily
        resultVar = newLocal(Type.getType("Ljavax/net/ssl/SSLEngineResult;"));
        mv.visitVarInsn(ASTORE, resultVar);

        // Call JavaSsl.safeEngineUnwrapExit with all parameters and result
        mv.visitVarInsn(ALOAD, 0); // SSLEngine this
        mv.visitVarInsn(ALOAD, resultVar); // SSLEngineResult result
        mv.visitVarInsn(ALOAD, 1); // ByteBuffer[] srcs
        mv.visitVarInsn(ILOAD, 2); // int srcsOffset
        mv.visitVarInsn(ILOAD, 3); // int srcsLength
        mv.visitVarInsn(ALOAD, 4); // ByteBuffer[] dsts
        mv.visitVarInsn(ILOAD, 5); // int dstsOffset
        mv.visitVarInsn(ILOAD, 6); // int dstsLength
        mv.visitMethodInsn(
            INVOKESTATIC,
            "io/qpoint/qtap/JavaSsl",
            "safeEngineUnwrapExit",
            "(Ljavax/net/ssl/SSLEngine;Ljavax/net/ssl/SSLEngineResult;[Ljava/nio/ByteBuffer;II[Ljava/nio/ByteBuffer;II)V",
            false);

        // Put the return value back on the stack
        mv.visitVarInsn(ALOAD, resultVar);
      }

      // End of protected region
      mv.visitLabel(tryEnd);
      mv.visitJumpInsn(GOTO, continueExecution);

      // Exception handler
      mv.visitLabel(catchHandler);
      int exceptionVar = newLocal(Type.getType(Throwable.class));
      mv.visitVarInsn(ASTORE, exceptionVar);

      // If we were handling ARETURN and resultVar was initialized, put the value back
      if (opcode == ARETURN && resultVar >= 0) {
        mv.visitVarInsn(ALOAD, resultVar);
      }

      // Continue returning
      mv.visitLabel(continueExecution);
    }
  }
}
