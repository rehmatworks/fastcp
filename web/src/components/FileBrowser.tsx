import React, { useState, useEffect, useCallback } from 'react'
import { api, FileInfo, FileContent } from '../lib/api'

const FileBrowser: React.FC<{ siteId: string }> = ({ siteId }) => {
    const [currentPath, setCurrentPath] = useState('.')
    const [files, setFiles] = useState<FileInfo[]>([])
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<string | null>(null)
    const [canRead, setCanRead] = useState(false)
    const [canWrite, setCanWrite] = useState(false)

    // File editing state
    const [editingFile, setEditingFile] = useState<FileContent | null>(null)
    const [editContent, setEditContent] = useState('')
    const [saving, setSaving] = useState(false)

    // Upload state
    const [uploading, setUploading] = useState(false)
    const [uploadProgress, setUploadProgress] = useState(0)

    // Context menu state
    const [contextMenu, setContextMenu] = useState<{
        x: number
        y: number
        file: FileInfo | null
    } | null>(null)

    // Modal states
    const [showCreateDir, setShowCreateDir] = useState(false)
    const [newDirName, setNewDirName] = useState('')
    const [creatingDir, setCreatingDir] = useState(false)

    const loadFiles = useCallback(async (path: string = currentPath) => {
        try {
            setLoading(true)
            setError(null)
            const response = await api.getFiles(siteId, path)
            setFiles(response.files)
            setCanRead(response.can_read)
            setCanWrite(response.can_write)
            setCurrentPath(response.path)
        } catch (err: any) {
            setError(err.message || 'Failed to load files')
            console.error('Error loading files:', err)
        } finally {
            setLoading(false)
        }
    }, [siteId, currentPath])

    useEffect(() => {
        loadFiles()
    }, [loadFiles])

    const navigateToPath = (path: string) => {
        setCurrentPath(path)
        loadFiles(path)
    }

    const goUp = () => {
        if (currentPath !== '.') {
            const parentPath = currentPath.split('/').slice(0, -1).join('/') || '.'
            navigateToPath(parentPath)
        }
    }

    const formatFileSize = (bytes: number): string => {
        if (bytes === 0) return '0 B'
        const k = 1024
        const sizes = ['B', 'KB', 'MB', 'GB']
        const i = Math.floor(Math.log(bytes) / Math.log(k))
        return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i]
    }

    const formatDate = (dateString: string): string => {
        return new Date(dateString).toLocaleString()
    }

    const handleFileClick = (file: FileInfo) => {
        if (file.is_dir) {
            navigateToPath(file.path)
        } else {
            // For now, just show context menu or open editor
            handleContextMenu(file, 0, 0)
        }
    }

    const handleContextMenu = (file: FileInfo | null, x: number, y: number) => {
        setContextMenu({ x, y, file })
    }

    const closeContextMenu = () => {
        setContextMenu(null)
    }

    const handleEditFile = async (file: FileInfo) => {
        try {
            const content = await api.getFileContent(siteId, file.path)
            setEditingFile(content)
            setEditContent(content.content)
            closeContextMenu()
        } catch (err: any) {
            alert('Failed to load file: ' + (err.message || 'Unknown error'))
        }
    }

    const handleSaveFile = async () => {
        if (!editingFile) return

        try {
            setSaving(true)
            await api.saveFileContent(siteId, editingFile.path, editContent)
            setEditingFile(null)
            loadFiles() // Refresh file list
        } catch (err: any) {
            alert('Failed to save file: ' + (err.message || 'Unknown error'))
        } finally {
            setSaving(false)
        }
    }

    const handleDownloadFile = (file: FileInfo) => {
        const link = document.createElement('a')
        link.href = `/api/v1/sites/${siteId}/files/download?path=${encodeURIComponent(file.path)}`
        link.download = file.name
        document.body.appendChild(link)
        link.click()
        document.body.removeChild(link)
        closeContextMenu()
    }

    const handleDeleteFile = async (file: FileInfo) => {
        if (!confirm(`Are you sure you want to delete "${file.name}"?`)) return

        try {
            await api.deleteFile(siteId, file.path)
            loadFiles() // Refresh file list
            closeContextMenu()
        } catch (err: any) {
            alert('Failed to delete file: ' + (err.message || 'Unknown error'))
        }
    }

    const handleCreateDirectory = async () => {
        if (!newDirName.trim()) return

        try {
            setCreatingDir(true)
            await api.createDirectory(siteId, currentPath, newDirName.trim())
            setShowCreateDir(false)
            setNewDirName('')
            loadFiles() // Refresh file list
        } catch (err: any) {
            alert('Failed to create directory: ' + (err.message || 'Unknown error'))
        } finally {
            setCreatingDir(false)
        }
    }

    const handleFileUpload = async (event: React.ChangeEvent<HTMLInputElement>) => {
        const fileList = event.target.files
        if (!fileList || fileList.length === 0) return

        const formData = new FormData()
        for (let i = 0; i < fileList.length; i++) {
            formData.append('files', fileList[i])
        }

        try {
            setUploading(true)
            setUploadProgress(0)

            // Use fetch directly for file upload with progress
            const response = await fetch(`/api/v1/sites/${siteId}/files/upload?path=${encodeURIComponent(currentPath)}`, {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${localStorage.getItem('fastcp_token')}`
                },
                body: formData
            })

            if (!response.ok) {
                const error = await response.json()
                throw new Error(error.error || 'Upload failed')
            }

            const result = await response.json()
            alert(`Successfully uploaded ${result.uploaded} file(s)`)
            loadFiles() // Refresh file list
        } catch (err: any) {
            alert('Failed to upload files: ' + (err.message || 'Unknown error'))
        } finally {
            setUploading(false)
            setUploadProgress(0)
            // Clear the input
            event.target.value = ''
        }
    }

    const getBreadcrumbs = () => {
        if (currentPath === '.') return [{ name: 'Root', path: '.' }]

        const parts = currentPath.split('/').filter(p => p)
        const breadcrumbs = [{ name: 'Root', path: '.' }]

        let current = '.'
        for (const part of parts) {
            current = current === '.' ? part : `${current}/${part}`
            breadcrumbs.push({ name: part, path: current })
        }

        return breadcrumbs
    }

    if (loading && files.length === 0) {
        return (
            <div className="bg-gray-900 rounded-lg border border-gray-800 p-8">
                <div className="flex items-center justify-center">
                    <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-emerald-500"></div>
                    <span className="ml-3 text-gray-400">Loading files...</span>
                </div>
            </div>
        )
    }

    if (error) {
        return (
            <div className="bg-gray-900 rounded-lg border border-gray-800 p-8">
                <div className="text-center">
                    <div className="text-red-400 mb-4">⚠️ {error}</div>
                    <button
                        onClick={() => loadFiles()}
                        className="px-4 py-2 bg-emerald-600 hover:bg-emerald-700 text-white rounded-lg transition-colors"
                    >
                        Retry
                    </button>
                </div>
            </div>
        )
    }

    return (
        <div className="bg-gray-900 rounded-lg border border-gray-800 overflow-hidden">
            {/* Toolbar */}
            <div className="border-b border-gray-800 p-4">
                <div className="flex items-center justify-between mb-4">
                    <div className="flex items-center gap-2">
                        <button
                            onClick={goUp}
                            disabled={currentPath === '.'}
                            className="px-3 py-1 bg-gray-800 hover:bg-gray-700 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded text-sm transition-colors"
                        >
                            ↑ Up
                        </button>
                        <button
                            onClick={() => loadFiles()}
                            className="px-3 py-1 bg-gray-800 hover:bg-gray-700 text-white rounded text-sm transition-colors"
                        >
                            ↻ Refresh
                        </button>
                    </div>

                    {canWrite && (
                        <div className="flex items-center gap-2">
                            <button
                                onClick={() => setShowCreateDir(true)}
                                className="px-3 py-1 bg-emerald-600 hover:bg-emerald-700 text-white rounded text-sm transition-colors"
                            >
                                📁 New Folder
                            </button>
                            <label className="px-3 py-1 bg-blue-600 hover:bg-blue-700 text-white rounded text-sm cursor-pointer transition-colors">
                                📤 Upload
                                <input
                                    type="file"
                                    multiple
                                    onChange={handleFileUpload}
                                    className="hidden"
                                    disabled={uploading}
                                />
                            </label>
                        </div>
                    )}
                </div>

                {/* Breadcrumbs */}
                <div className="flex items-center gap-1 text-sm">
                    {getBreadcrumbs().map((crumb, index) => (
                        <React.Fragment key={crumb.path}>
                            {index > 0 && <span className="text-gray-500">/</span>}
                            <button
                                onClick={() => navigateToPath(crumb.path)}
                                className="text-emerald-400 hover:text-emerald-300 transition-colors"
                            >
                                {crumb.name}
                            </button>
                        </React.Fragment>
                    ))}
                </div>
            </div>

            {/* File List */}
            <div className="overflow-x-auto">
                <table className="w-full">
                    <thead className="bg-gray-800/50">
                        <tr>
                            <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">Name</th>
                            <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">Size</th>
                            <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">Modified</th>
                            <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">Permissions</th>
                            <th className="px-4 py-3 text-left text-xs font-medium text-gray-400 uppercase tracking-wider">Actions</th>
                        </tr>
                    </thead>
                    <tbody className="divide-y divide-gray-800">
                        {files.map((file) => (
                            <tr
                                key={file.path}
                                className="hover:bg-gray-800/30 cursor-pointer"
                                onClick={() => handleFileClick(file)}
                                onContextMenu={(e) => {
                                    e.preventDefault()
                                    handleContextMenu(file, e.clientX, e.clientY)
                                }}
                            >
                                <td className="px-4 py-3">
                                    <div className="flex items-center gap-3">
                                        <span className="text-lg">
                                            {file.is_dir ? '📁' : '📄'}
                                        </span>
                                        <span className="text-white font-medium">{file.name}</span>
                                    </div>
                                </td>
                                <td className="px-4 py-3 text-sm text-gray-400">
                                    {file.is_dir ? '--' : formatFileSize(file.size)}
                                </td>
                                <td className="px-4 py-3 text-sm text-gray-400">
                                    {formatDate(file.mod_time)}
                                </td>
                                <td className="px-4 py-3 text-sm text-gray-400 font-mono">
                                    {file.perm}
                                </td>
                                <td className="px-4 py-3">
                                    <div className="flex items-center gap-2">
                                        {!file.is_dir && canRead && (
                                            <button
                                                onClick={(e) => {
                                                    e.stopPropagation()
                                                    handleEditFile(file)
                                                }}
                                                className="px-2 py-1 bg-blue-600 hover:bg-blue-700 text-white text-xs rounded transition-colors"
                                            >
                                                Edit
                                            </button>
                                        )}
                                        {canRead && (
                                            <button
                                                onClick={(e) => {
                                                    e.stopPropagation()
                                                    handleDownloadFile(file)
                                                }}
                                                className="px-2 py-1 bg-green-600 hover:bg-green-700 text-white text-xs rounded transition-colors"
                                            >
                                                Download
                                            </button>
                                        )}
                                        {canWrite && (
                                            <button
                                                onClick={(e) => {
                                                    e.stopPropagation()
                                                    handleDeleteFile(file)
                                                }}
                                                className="px-2 py-1 bg-red-600 hover:bg-red-700 text-white text-xs rounded transition-colors"
                                            >
                                                Delete
                                            </button>
                                        )}
                                    </div>
                                </td>
                            </tr>
                        ))}
                    </tbody>
                </table>

                {files.length === 0 && (
                    <div className="text-center py-12">
                        <div className="text-gray-500 mb-2">📂</div>
                        <p className="text-gray-400">This directory is empty</p>
                    </div>
                )}
            </div>

            {/* Upload Progress */}
            {uploading && (
                <div className="border-t border-gray-800 p-4">
                    <div className="flex items-center gap-3">
                        <div className="flex-1 bg-gray-800 rounded-full h-2">
                            <div
                                className="bg-emerald-500 h-2 rounded-full transition-all duration-300"
                                style={{ width: `${uploadProgress}%` }}
                            ></div>
                        </div>
                        <span className="text-sm text-gray-400">Uploading...</span>
                    </div>
                </div>
            )}

            {/* Context Menu */}
            {contextMenu && (
                <div
                    className="fixed z-50 bg-gray-800 border border-gray-700 rounded-lg shadow-lg py-1 min-w-48"
                    style={{ left: contextMenu.x, top: contextMenu.y }}
                    onClick={(e) => e.stopPropagation()}
                >
                    {contextMenu.file && (
                        <>
                            {!contextMenu.file.is_dir && canRead && (
                                <button
                                    onClick={() => handleEditFile(contextMenu.file!)}
                                    className="w-full text-left px-3 py-2 text-sm text-white hover:bg-gray-700 transition-colors flex items-center gap-2"
                                >
                                    ✏️ Edit
                                </button>
                            )}
                            {canRead && (
                                <button
                                    onClick={() => handleDownloadFile(contextMenu.file!)}
                                    className="w-full text-left px-3 py-2 text-sm text-white hover:bg-gray-700 transition-colors flex items-center gap-2"
                                >
                                    📥 Download
                                </button>
                            )}
                            {canWrite && (
                                <>
                                    <div className="border-t border-gray-700 my-1"></div>
                                    <button
                                        onClick={() => handleDeleteFile(contextMenu.file!)}
                                        className="w-full text-left px-3 py-2 text-sm text-red-400 hover:bg-gray-700 transition-colors flex items-center gap-2"
                                    >
                                        🗑️ Delete
                                    </button>
                                </>
                            )}
                        </>
                    )}
                </div>
            )}

            {/* Click outside to close context menu */}
            {contextMenu && (
                <div
                    className="fixed inset-0 z-40"
                    onClick={closeContextMenu}
                    onContextMenu={(e) => {
                        e.preventDefault()
                        closeContextMenu()
                    }}
                />
            )}

            {/* Create Directory Modal */}
            {showCreateDir && (
                <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
                    <div className="bg-gray-900 border border-gray-800 rounded-lg p-6 w-full max-w-md">
                        <h3 className="text-lg font-semibold text-white mb-4">Create New Directory</h3>
                        <input
                            type="text"
                            value={newDirName}
                            onChange={(e) => setNewDirName(e.target.value)}
                            placeholder="Directory name"
                            className="w-full px-3 py-2 bg-gray-800 border border-gray-700 rounded text-white placeholder-gray-400 focus:outline-none focus:border-emerald-500"
                            onKeyDown={(e) => {
                                if (e.key === 'Enter') handleCreateDirectory()
                                if (e.key === 'Escape') setShowCreateDir(false)
                            }}
                            autoFocus
                        />
                        <div className="flex gap-3 mt-6">
                            <button
                                onClick={() => setShowCreateDir(false)}
                                className="flex-1 px-4 py-2 bg-gray-800 hover:bg-gray-700 text-white rounded transition-colors"
                            >
                                Cancel
                            </button>
                            <button
                                onClick={handleCreateDirectory}
                                disabled={!newDirName.trim() || creatingDir}
                                className="flex-1 px-4 py-2 bg-emerald-600 hover:bg-emerald-700 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded transition-colors"
                            >
                                {creatingDir ? 'Creating...' : 'Create'}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* File Editor Modal */}
            {editingFile && (
                <div className="fixed inset-0 bg-black/50 flex items-center justify-center z-50">
                    <div className="bg-gray-900 border border-gray-800 rounded-lg w-full max-w-4xl h-4/5 flex flex-col">
                        <div className="border-b border-gray-800 p-4">
                            <div className="flex items-center justify-between">
                                <h3 className="text-lg font-semibold text-white">
                                    Edit: {editingFile.path}
                                </h3>
                                <div className="flex items-center gap-2 text-sm text-gray-400">
                                    <span>Size: {formatFileSize(editingFile.size)}</span>
                                    {!editingFile.can_edit && (
                                        <span className="text-yellow-400">⚠️ Read-only</span>
                                    )}
                                </div>
                            </div>
                        </div>

                        <div className="flex-1 p-4">
                            <textarea
                                value={editContent}
                                onChange={(e) => setEditContent(e.target.value)}
                                disabled={!editingFile.can_edit}
                                className="w-full h-full bg-gray-800 border border-gray-700 rounded text-white font-mono text-sm p-3 focus:outline-none focus:border-emerald-500 resize-none"
                                placeholder={editingFile.can_edit ? "File content..." : "This file is read-only"}
                            />
                        </div>

                        <div className="border-t border-gray-800 p-4 flex justify-end gap-3">
                            <button
                                onClick={() => setEditingFile(null)}
                                className="px-4 py-2 bg-gray-800 hover:bg-gray-700 text-white rounded transition-colors"
                            >
                                Cancel
                            </button>
                            {editingFile.can_edit && (
                                <button
                                    onClick={handleSaveFile}
                                    disabled={saving}
                                    className="px-4 py-2 bg-emerald-600 hover:bg-emerald-700 disabled:opacity-50 disabled:cursor-not-allowed text-white rounded transition-colors"
                                >
                                    {saving ? 'Saving...' : 'Save Changes'}
                                </button>
                            )}
                        </div>
                    </div>
                </div>
            )}
        </div>
    )
}

export default FileBrowser