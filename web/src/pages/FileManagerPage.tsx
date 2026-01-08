import React, { useState, useEffect } from 'react'
import { useParams, useNavigate } from 'react-router-dom'
import { Layout } from '../components/Layout'
import FileBrowser from '../components/FileBrowser'
import { api } from '../lib/api'
import { Site } from '../types'

const FileManagerPage: React.FC = () => {
    const { siteId } = useParams<{ siteId: string }>()
    const navigate = useNavigate()
    const [site, setSite] = useState<Site | null>(null)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<string | null>(null)

    useEffect(() => {
        if (!siteId) {
            navigate('/sites')
            return
        }

        loadSite()
    }, [siteId, navigate])

    const loadSite = async () => {
        try {
            setLoading(true)
            const siteData = await api.getSite(siteId!)
            setSite(siteData)
        } catch (err) {
            setError('Failed to load site information')
            console.error('Error loading site:', err)
        } finally {
            setLoading(false)
        }
    }

    if (loading) {
        return (
            <Layout>
                <div className="flex items-center justify-center min-h-96">
                    <div className="animate-spin rounded-full h-8 w-8 border-b-2 border-emerald-500"></div>
                </div>
            </Layout>
        )
    }

    if (error || !site) {
        return (
            <Layout>
                <div className="max-w-4xl mx-auto">
                    <div className="bg-red-500/10 border border-red-500/20 rounded-lg p-4">
                        <p className="text-red-400">{error || 'Site not found'}</p>
                        <button
                            onClick={() => navigate('/sites')}
                            className="mt-2 text-sm text-emerald-400 hover:text-emerald-300"
                        >
                            ← Back to Sites
                        </button>
                    </div>
                </div>
            </Layout>
        )
    }

    return (
        <Layout>
            <div className="max-w-7xl mx-auto">
                {/* Header */}
                <div className="mb-6">
                    <div className="flex items-center gap-4 mb-2">
                        <button
                            onClick={() => navigate('/sites')}
                            className="text-gray-400 hover:text-white transition-colors"
                        >
                            ← Sites
                        </button>
                        <span className="text-gray-500">/</span>
                        <button
                            onClick={() => navigate(`/sites/${site.id}`)}
                            className="text-gray-400 hover:text-white transition-colors"
                        >
                            {site.name}
                        </button>
                        <span className="text-gray-500">/</span>
                        <span className="text-white">File Manager</span>
                    </div>

                    <div className="flex items-center justify-between">
                        <div>
                            <h1 className="text-2xl font-bold text-white">File Manager</h1>
                            <p className="text-gray-400 mt-1">
                                Manage files for {site.domain}
                            </p>
                        </div>

                        <div className="flex items-center gap-2 text-sm text-gray-400">
                            <span>Site:</span>
                            <span className="text-white font-medium">{site.name}</span>
                            <span className="text-gray-500">•</span>
                            <span>Domain:</span>
                            <span className="text-emerald-400">{site.domain}</span>
                        </div>
                    </div>
                </div>

                {/* File Browser */}
                <FileBrowser siteId={site.id} />
            </div>
        </Layout>
    )
}

export default FileManagerPage