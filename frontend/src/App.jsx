import { useState, useEffect, useCallback } from 'react';

const API_BASE = '/api/items';

// ---------------------------------------------------------------------------
// Styles
// Inline style objects keep the component self-contained for Step 1.
// ---------------------------------------------------------------------------
const styles = {
  container: {
    maxWidth: '600px',
    margin: '0 auto',
  },
  heading: {
    fontSize: '1.75rem',
    fontWeight: '700',
    marginBottom: '1.5rem',
  },
  form: {
    display: 'flex',
    gap: '0.5rem',
    marginBottom: '1.5rem',
  },
  input: {
    flex: 1,
    padding: '0.5rem 0.75rem',
    fontSize: '1rem',
    border: '1px solid #ccc',
    borderRadius: '4px',
  },
  button: {
    padding: '0.5rem 1.25rem',
    fontSize: '1rem',
    background: '#2563eb',
    color: '#fff',
    border: 'none',
    borderRadius: '4px',
    cursor: 'pointer',
  },
  buttonDisabled: {
    opacity: 0.6,
    cursor: 'not-allowed',
  },
  error: {
    background: '#fee2e2',
    border: '1px solid #fca5a5',
    color: '#991b1b',
    padding: '0.75rem 1rem',
    borderRadius: '4px',
    marginBottom: '1rem',
    fontSize: '0.9rem',
  },
  list: {
    listStyle: 'none',
    display: 'flex',
    flexDirection: 'column',
    gap: '0.5rem',
  },
  listItem: {
    background: '#fff',
    border: '1px solid #e5e7eb',
    borderRadius: '4px',
    padding: '0.75rem 1rem',
    fontSize: '1rem',
  },
  emptyState: {
    color: '#6b7280',
    fontStyle: 'italic',
  },
  timestamp: {
    fontSize: '0.75rem',
    color: '#9ca3af',
    marginTop: '0.25rem',
  },
};

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------
export default function App() {
  const [items, setItems] = useState([]);
  const [title, setTitle] = useState('');
  const [loading, setLoading] = useState(false);
  const [fetchError, setFetchError] = useState(null);
  const [submitError, setSubmitError] = useState(null);

  // Fetch the current list of items from the API.
  const fetchItems = useCallback(async () => {
    setFetchError(null);
    try {
      const res = await fetch(API_BASE);
      if (!res.ok) {
        throw new Error(`Server responded with ${res.status}`);
      }
      const data = await res.json();
      setItems(data.items);
    } catch (err) {
      setFetchError(`Could not load items: ${err.message}`);
    }
  }, []);

  useEffect(() => {
    fetchItems();
  }, [fetchItems]);

  // Submit a new item to the API.
  async function handleSubmit(e) {
    e.preventDefault();
    setSubmitError(null);

    const trimmed = title.trim();
    if (!trimmed) {
      setSubmitError('Title cannot be empty.');
      return;
    }

    setLoading(true);
    try {
      const res = await fetch(API_BASE, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: trimmed }),
      });

      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        throw new Error(data?.error?.message ?? `Server responded with ${res.status}`);
      }

      setTitle('');
      // Refresh the list from the server so the UI reflects the real DB state.
      await fetchItems();
    } catch (err) {
      setSubmitError(`Could not add item: ${err.message}`);
    } finally {
      setLoading(false);
    }
  }

  return (
    <div style={styles.container}>
      <h1 style={styles.heading}>DevOps Sentinel - Items</h1>

      {/* Add item form */}
      <form onSubmit={handleSubmit} style={styles.form} aria-label="Add item form">
        <input
          style={styles.input}
          type="text"
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          placeholder="Enter a task title..."
          aria-label="Task title"
          maxLength={200}
          disabled={loading}
        />
        <button
          type="submit"
          style={loading ? { ...styles.button, ...styles.buttonDisabled } : styles.button}
          disabled={loading}
          aria-disabled={loading}
        >
          {loading ? 'Adding...' : 'Add'}
        </button>
      </form>

      {/* Submission error */}
      {submitError && (
        <div role="alert" style={styles.error}>
          {submitError}
        </div>
      )}

      {/* Fetch error */}
      {fetchError && (
        <div role="alert" style={styles.error}>
          {fetchError}
        </div>
      )}

      {/* Items list */}
      {items.length === 0 && !fetchError ? (
        <p style={styles.emptyState}>No items yet. Add one above.</p>
      ) : (
        <ul style={styles.list} aria-label="Items list">
          {items.map((item) => (
            <li key={item._id} style={styles.listItem}>
              <div>{item.title}</div>
              <div style={styles.timestamp}>
                {new Date(item.createdAt).toLocaleString()}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
