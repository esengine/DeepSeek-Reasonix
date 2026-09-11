export const sleep = (ms) => new Promise((done) => setTimeout(done, ms));

export function processAlive(pid) {
  if (!Number.isInteger(pid) || pid <= 0) throw new Error(`invalid process id: ${pid}`);
  try {
    process.kill(pid, 0);
    return true;
  } catch (error) {
    if (error.code === "ESRCH") return false;
    throw error;
  }
}

export async function waitForProcessesToExit(pids, timeout = 5_000) {
  const deadline = Date.now() + timeout;
  let remaining;
  while ((remaining = pids.filter(processAlive)).length > 0) {
    if (Date.now() >= deadline) throw new Error(`processes outlived normal app quit: ${remaining.join(", ")}`);
    await sleep(100);
  }
}

// ElectronApplication.close invokes app.quit and waits for the application to
// close. A timeout is a failed smoke, never evidence that forced cleanup worked.
export async function closeAndVerify(application, { shellPid, servicePid }, timeout = 15_000) {
  let timer;
  try {
    await Promise.race([
      application.close(),
      new Promise((_, reject) => {
        timer = setTimeout(() => reject(new Error(`normal app quit did not complete within ${timeout / 1000}s`)), timeout);
      }),
    ]);
  } finally {
    clearTimeout(timer);
  }
  await waitForProcessesToExit([shellPid, servicePid]);
}
