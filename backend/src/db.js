/**
 * db.js
 * Manages the Mongoose connection lifecycle.
 * Exports connect() and isConnected() so the health endpoint
 * can report real database state without importing mongoose directly.
 */

import mongoose from 'mongoose';

/**
 * Opens the MongoDB connection.
 * Throws on failure so the caller (index.js) can decide whether to exit.
 *
 * @param {string} uri - MongoDB connection string from environment.
 */
export async function connect(uri) {
  await mongoose.connect(uri, {
    // These are the recommended settings for Mongoose 8.
    serverSelectionTimeoutMS: 5000,
    socketTimeoutMS: 45000,
  });
}

/**
 * Returns true when Mongoose reports a live connection (state === 1).
 * Used by the /health endpoint.
 */
export function isConnected() {
  return mongoose.connection.readyState === 1;
}

/**
 * Closes the Mongoose connection gracefully.
 * Called during SIGTERM handling.
 */
export async function disconnect() {
  await mongoose.connection.close();
}
