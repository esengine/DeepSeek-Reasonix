// English for the storage panel: what is on disk, where it lives, and moving
// it. Split out of en.ts because that file is one screen's worth of catalogue
// per section and had grown past its ceiling — the grouping is the same.
export const EN_STORAGE: Record<string, string> = {
  "storage.recoverOlderSessions": "Recover older sessions",
  "storage.importFromAnOlderReasonix": "Import from an older Reasonix installation",
  "storage.chooseTheOlderDataFolder":
    "Choose the older data folder. The scan only copies sessions and never moves or deletes the originals; sessions whose workspace no longer exists are placed in the current workspace.",
  "storage.chooseOlderDataFolder": "Choose older data folder…",
  "storage.theFolderPanelCouldNot":
    "The folder panel could not be opened. Type the path of the older data folder (the one holding sessions, projects or desktop-sessions-v5).",
  "storage.olderDataFolderPath": "Older data folder path",
  "storage.importThisFolder": "Import this folder",
  "storage.scanning": "Scanning…",
  "storage.recoveredSessions": "Recovered {n} sessions.",
  "storage.nothingNewToImport": "Nothing new to import.",
  "storage.noOlderSessionsWereFound":
    "No older sessions were found in this folder. Pick the Reasonix data folder (the one holding sessions, projects or desktop-sessions-v5).",
  "storage.itemsCouldNotBeRead": "{n} items could not be read.",
  "legacySkips.copyPath": "Copy path: {name}",
  "legacySkips.sessionsWereNotImported": "{n} sessions were not imported",
  "legacySkips.theseFilesAreStillWhere":
    "These files are still where they were; nothing was moved or deleted. Copy a path to deal with it yourself, or fix it and import again.",
  "legacySkips.largerThanTheSizeLimit": "Larger than the size limit for reading",
  "legacySkips.notASessionFormatThis": "Not a session format this version can read",
  "legacySkips.itRecordsNoProjectIt": "It records no project it belongs to, so it was not imported",
  "legacySkips.writtenByAStorageVersion": "Written by a storage version this version does not support yet",
  "legacySkips.noPermissionToReadIt": "No permission to read it",
  "legacySkips.theFileIsDamaged": "The file is damaged",
  "legacySkips.copyingItToTheNew": "Copying it to the new location failed",
  "storage.thisFolderCouldNotBe": "This folder could not be scanned. Make sure it is an older Reasonix data folder.",
  "common.storage": "Storage",
  "settings.whereTheDataIsWritten":
    "Where the data is written and how much space it uses. Sessions and indexes keep growing while configuration and credentials do not, so only the former can be moved. A move takes effect after a restart.",
  "storage.cannotReadWhatIsOn": "Cannot read what is on disk.",
  "storage.measuring": "Measuring…",
  "storage.measuringTimedOut": "Measuring timed out",
  "storage.thisFolderIsTooLarge": "This folder is too large to finish measuring in time; the real size is at least this much.",
  "common.spaceUsed": "Space used",
  "storage.dataRemainsInThePrevious": "Data remains in the previous location",
  "storage.theseAreStillInA":
    "These are still in {dir}: {names}. A move left them behind, so the project list, global instructions and memory, scheduled runs, a wallpaper, a theme pack or the backups an update rolls back to can look gone on this machine. The next launch merges them into the current location; if they are still listed, quit and copy them there without overwriting files that already exist.",
  "common.locations": "Locations",
  "common.migrate": "Migrate",
  "storage.theMoveCouldNotBe": "The move could not be started.",
  "storage.freeOf": "{drive} — {free} free of {total}",
  "storage.setBy": "set by {env}",
  "storage.cannotBeMoved": "cannot be moved",
  "storage.move": "Move…",
  "storage.startTheMigration": "Start the migration",
  "storage.pointItHere": "Point it here",
  "storage.fullPathToTheDestination":
    "Full path to the destination folder — an empty one, or the one that already holds this data",
  "storage.movesInFilesTheDestination":
    "Moves {size} in {n} files; the destination has {free} free. Takes effect after a restart.",
  "storage.thisFolderAlreadyHoldsThat":
    "This folder already holds that data ({size} in {n} files). It will simply be pointed at — nothing is copied and nothing is deleted. Takes effect after a restart.",
  "storage.theCurrentLocationStillHolds":
    "The current location still holds {size}, which is not carried across.",
  "storage.movedItTakesEffectAfter": "Moved. It takes effect after a restart.",
  "storage.pointingAtTheNewLocation": "Pointing at the new location",
  "storage.copying": "Copying",
  "storage.verifying": "Verifying",
  "storage.committedClearingTheOldLocation": "Committed; clearing the old location",
  "storage.sessionsAndArchives": "Sessions and archives",
  "storage.transcriptsCompactionArchivesUsageSt":
    "Transcripts, compaction archives, usage stats, rewind snapshots",
  "storage.indexesAndCaches": "Indexes and caches",
  "storage.searchIndexesAndDerivedData":
    "Search indexes and derived data; deleting them rebuilds them",
  "storage.isolatedWorktrees": "Isolated worktrees",
  "storage.theSeparateCheckoutsDeliveryWorks": "The separate checkouts Delivery works in",
  "storage.configurationAndCredentials": "Configuration and credentials",
  "storage.settingsAndAPIKeysThese":
    "Settings and API keys; these always stay with your user profile",
  "storage.processLocks": "Process locks",
  "storage.howInstancesExcludeEachOther":
    "How instances exclude each other; these must stay in one place on this machine. Each is an empty file and removing one would break the exclusion, so they are kept rather than cleaned",
};
