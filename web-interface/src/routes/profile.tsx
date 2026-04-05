import { createFileRoute } from '@tanstack/react-router'
import { SquarePen } from 'lucide-react'
import { getUser } from '#/util.ts'
import { useImmer } from 'use-immer'
import { Button } from '#/components/Buttons.tsx'
import { useRef } from 'react'
import type { ChangeEvent } from 'react'

export const Route = createFileRoute('/profile')({
  component: ProfilePage,
  loader: getUser,
})

function ProfileField({
  label,
  value,
  type = 'text',
  onChange,
  error,
}: {
  label: string
  value: string
  type?: string
  onChange: (val: string) => void
  error?: string
}) {
  return (
    <div>
      <label className="block text-base font-medium mb-1">{label}</label>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        className={`w-full border ${error ? 'border-red-600' : 'border-gray-300'} rounded-lg px-4 py-2 text-sm disabled:bg-white disabled:text-gray-700`}
      />
      <p className="text-red-600 min-h-6">{error}</p>
    </div>
  )
}

function ProfilePage() {
  const loaderData = Route.useLoaderData()
  const [user, setUser] = useImmer({
    ...loaderData,
    password: '',
    confirmPassword: '',
  })
  const avatarUploadRef = useRef<HTMLInputElement>(null)

  function handleAvatarUpload(event: ChangeEvent<HTMLInputElement>) {
    const file = event.target.files?.[0]

    if (!file) return

    // TODO: call API to upload avatar
    console.log(`Uploaded image ${file.name}`)
  }

  function handleSave() {
    // TODO: call API to update profile
    console.log('Save profile:', {
      username: user.username,
      role: user.role,
      email: user.email,
      password: user.password,
    })
  }

  function handleCancel() {
    const confirmCancel = confirm('Are you sure you want to cancel?')

    if (confirmCancel) {
      setUser({
        ...loaderData,
        password: '',
        confirmPassword: '',
      })
    }
  }

  function getUserErrors(field: string): string | undefined {
    switch (field) {
      case 'username':
      case 'role':
      case 'email':
        if (user[field] === '') {
          return `Field cannot be empty.`
        }
        break
      case 'password':
        if (user['password'].length !== 0) {
          if (user['password'].length < 8) {
            return 'Password must be at least 8 characters long'
          }
        }
        break
      case 'confirmPassword':
        if (
          user['password'].length !== 0 &&
          user['confirmPassword'] !== user['password']
        ) {
          return 'Passwords do not match.'
        }
        break
    }
  }

  function hasError(): boolean {
    for (const field in user) {
      if (getUserErrors(field)) {
        return true
      }
    }

    return false
  }

  return (
    <div className="w-fit mx-auto py-8 px-8">
      <div className="flex gap-16 items-start">
        {/* Left: form fields */}
        <div className="flex-1 min-w-80 flex flex-col">
          <ProfileField
            label="Username"
            value={user.username}
            onChange={(username) =>
              setUser((draft) => {
                draft.username = username
              })
            }
            error={getUserErrors('username')}
          />
          <ProfileField
            label="Role"
            value={user.role}
            onChange={(role) =>
              setUser((draft) => {
                draft.role = role
              })
            }
            error={getUserErrors('role')}
          />
          <ProfileField
            label="Email"
            value={user.email}
            type="email"
            onChange={(email) =>
              setUser((draft) => {
                draft.email = email
              })
            }
            error={getUserErrors('email')}
          />
          <ProfileField
            label="Password"
            value={user.password}
            type="password"
            onChange={(password) =>
              setUser((draft) => {
                draft.password = password
              })
            }
            error={getUserErrors('password')}
          />
          <ProfileField
            label="Confirm Password"
            value={user.confirmPassword}
            type="password"
            onChange={(confirmPassword) =>
              setUser((draft) => {
                draft.confirmPassword = confirmPassword
              })
            }
            error={getUserErrors('confirmPassword')}
          />

          <div className="flex gap-3 mt-2">
            <Button
              onClick={handleSave}
              variant={hasError() ? 'disabled' : 'normal'}
              fontSize="base"
            >
              Save
            </Button>
            <Button onClick={handleCancel} variant="danger" fontSize="base">
              Cancel
            </Button>
          </div>
        </div>

        {/* Right: profile picture */}
        <div className="flex flex-col items-center gap-3 pt-6">
          <div className="relative">
            <img
              src={user.profilePicture}
              alt="Profile Picture"
              className="w-48 h-48 rounded-full object-cover border-2 border-gray-200"
            />
            <div className="absolute bottom-3 left-3">
              <Button
                onClick={() => avatarUploadRef.current?.click()}
                variant="normal"
                fontSize="base"
              >
                Edit <SquarePen size={16} />
              </Button>
              <input
                type="file"
                className="hidden"
                accept="image/*"
                ref={avatarUploadRef}
                onChange={handleAvatarUpload}
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}
