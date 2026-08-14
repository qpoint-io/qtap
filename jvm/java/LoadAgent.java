package io.qpoint.qtap;

import com.sun.tools.attach.VirtualMachine;

/**
 * LoadAgent - JVM Agent Loader Utility
 *
 * <p>This utility provides functionality to attach to a running JVM process by its PID and
 * dynamically load either a native or Java agent into that process.
 *
 * <p>Key differences between agent types:
 *
 * <p>1. Native Agents (.so files): - Written in C/C++ and compiled to native shared libraries -
 * Loaded using VirtualMachine.loadAgentPath() - Interact with JVM through JNI and JVMTI interfaces
 * - Can access low-level JVM internals and native memory - Typically used for profiling, debugging,
 * and monitoring
 *
 * <p>2. Java Agents (.jar files): - Written in Java and packaged as JAR files - Loaded using
 * VirtualMachine.loadAgent() - Must contain a manifest with Agent-Class attribute - Operate at Java
 * level through Instrumentation API - Can transform bytecode, redefine classes, and monitor class
 * loading - Safer and more portable than native agents
 *
 * <p>This utility detects the agent type by file extension and uses the appropriate loading method.
 * It also handles common exceptions that may occur during the attachment and loading process.
 */
public class LoadAgent {
  public static void main(String[] args) throws Exception {
    if (args.length < 2) {
      System.out.println("Usage: LoadAgent <pid> <agentPath> [options]");
      System.exit(1);
    }

    String pid = args[0];
    String agentPath = args[1];
    String options = args.length > 2 ? args[2] : "";

    loadAgent(pid, agentPath, options);
  }

  static void loadAgent(String pid, String agentPath, String options) throws Exception {
    try {
      VirtualMachine vm = VirtualMachine.attach(pid);
      try {
        if (agentPath.endsWith(".jar")) {
          // Java agent: use loadAgent()
          vm.loadAgent(agentPath, options);
        } else {
          // Native agent: use loadAgentPath()
          vm.loadAgentPath(agentPath, options);
        }
      } catch (com.sun.tools.attach.AgentInitializationException e) {
        // rethrow all but the expected exception
        if (!e.getMessage().equals("Agent_OnAttach failed")) throw e;
      } finally {
        vm.detach();
      }
    } catch (com.sun.tools.attach.AttachNotSupportedException e) {
      System.out.println("Failed to attach to process " + pid + ": " + e.getMessage());
      throw new RuntimeException("Failed to attach to process " + pid, e);
    }
  }
}
